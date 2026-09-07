---
title: "Client-Held E2EE: Envelope Encryption Real"
date: 2026-08-11T16:36:53Z
draft: false
post_type: architecture
tags: [go, encryption, e2ee, sentinel]
categories: [encryption]
summary: "Envelope encryption with a client-held key: the server stores ciphertext and can't read it — across S3 and native transports."
artifacts:
  - {type: pr, id: "779"}
  - {type: pr, id: "781"}
  - {type: spec, path: openspec/changes/add-e2e-encryption}
related:
  - 014-confidential-dedup-oprf
  - 006-pluggable-storage-backends
  - 015-sentinel-security-audit
---

The strongest security guarantee a cloud storage system can offer is simple: **even if an attacker gains full root access to the storage nodes, physical disks, and metadata databases, they cannot read a single byte of customer data.**

momo achieves this through **Client-Held Envelope Encryption (E2EE)** across both its S3 gateway and native binary transports.

{{< diagram src="/diagrams/05-envelope-encryption.svg" alt="Envelope encryption overview" caption="Figure 1: High-level overview of client-held envelope encryption" >}}

---

## 1. The Key Hierarchy: KEK, CEK, and Envelopes

Why not encrypt all files directly with a single user password or master key?
- **Key Wear-Out**: Using one key to encrypt terabytes of data exhausts the cryptographic nonce space of AES-GCM (which must never repeat under the same key).
- **Rotation Nightmares**: If a master key is compromised, re-encrypting petabytes of data requires reading, decrypting, and rewriting every physical block on disk.

Envelope encryption solves this by decoupling the **Data Key** from the **Master Key**:

{{< diagram src="/diagrams/05a-key-hierarchy.svg" alt="Key hierarchy" caption="Figure 2: Cryptographic separation between Master KEK, Ephemeral CEK, and Wrapped CEK" >}}

1. **Content Encryption Key (CEK)**: A high-entropy 256-bit AES key generated randomly in client RAM for a single object.
2. **Key Encryption Key (KEK)**: The client's long-lived master key. **The KEK never leaves the client device.** It is never transmitted across the network and never stored in server configuration.
3. **Wrapped CEK**: The client encrypts the 32-byte CEK using its KEK (`AES-256-GCM.Seal(KEK, CEK)`). The resulting small encrypted envelope is sent alongside the ciphertext.

---

## 2. The Ingest (PUT) Pipeline

During an upload, encryption happens entirely before bytes cross the network boundary:

{{< diagram src="/diagrams/05b-put-flow.svg" alt="E2EE PUT Ingest flow" caption="Figure 3: Upload flow — client encrypts payload and wraps key before network transit" >}}

In `src/crypto/envelope.go`:

```go
type Envelope struct {
    KeyID      string `json:"key_id"`
    WrappedCEK []byte `json:"wrapped_cek"`
    Nonce      []byte `json:"nonce"`
}

// ClientEncrypt streams plaintext into AES-256-GCM ciphertext
func (c *Client) EncryptStream(plaintext io.Reader, masterKEK []byte) (io.Reader, *Envelope, error) {
    // 1. Generate random 32-byte CEK
    cek := make([]byte, 32)
    if _, err := rand.Read(cek); err != nil {
        return nil, nil, err
    }

    // 2. Wrap CEK with Master KEK
    wrappedCEK, nonce, err := sealKey(masterKEK, cek)
    if err != nil {
        return nil, nil, err
    }

    // 3. Construct streaming AES-GCM cipher over payload
    cipherStream, err := newGCMEncryptReader(plaintext, cek)
    if err != nil {
        return nil, nil, err
    }

    env := &Envelope{KeyID: "default", WrappedCEK: wrappedCEK, Nonce: nonce}
    return cipherStream, env, nil
}
```

The server receives only the raw AES-GCM ciphertext stream and the JSON envelope. It indexes the envelope in Bbolt metadata (`bucketS3Meta`) and writes the ciphertext directly into the CAS store.

---

## 3. The Retrieval (GET) Pipeline

When downloading, the process is inverted:

{{< diagram src="/diagrams/05c-get-flow.svg" alt="E2EE GET Retrieval flow" caption="Figure 4: Download flow — server returns ciphertext, client decrypts with private KEK" >}}

1. The server streams the raw ciphertext blob from disk and includes the `WrappedCEK` envelope in response headers.
2. The client receives the envelope, extracts the wrapped key, and unwraps it locally using its private KEK (`CEK = KEK.Open(WrappedCEK)`).
3. The client streams the ciphertext through an AES-GCM decrypting reader, verifying authentication tags in real time.

---

## 4. Tradeoff Analysis

| Metric / Dimension | Server-Side Encryption (SSE-S3) | Client-Held Envelope Encryption (E2EE) |
|---|---|---|
| **Server Trust Assumption** | Server holds keys; rogue admin can read data | **Zero trust**: Server holds only ciphertext |
| **CAS Deduplication** | Preserved (Plaintext hashes match) | Neutralized (Random CEK generates unique ciphertext) |
| **Key Rotation Overhead** | Requires re-encrypting all stored blobs | **$O(1)$ per object**: Re-wrap only the 32-byte CEK |
| **Client CPU Overhead** | Zero client CPU | Client performs AES-NI encryption/decryption |
| **Data Loss Risk on Key Loss**| Recoverable via server/cloud KMS | **Permanent**: Lost KEK means data is irrecoverable |

---

## 5. Security Invariants (🛡 Sentinel Mindset)

In accordance with [docs/STANDARDS.md](../../STANDARDS.md):
- 🛡 **Sentinel (Truncation Defense)**: Streaming AES-GCM carries an authenticated integrity footer. If an attacker truncates a file in transit or deletes trailing disk blocks, the client GCM tag verification fails with `syscall.EBADMSG`, preventing partial-read vulnerabilities.
- 🛡 **Fail-Closed Key Semantics**: If a client attempts to decrypt an object with an invalid KEK, decryption halts immediately. The server never attempts "fallback" plaintexts or heuristic recoveries.

## Related

- Pluggable storage backends: [006](006-pluggable-storage-backends.md)
- Confidential deduplication via OPRF: [014](014-confidential-dedup-oprf.md)
- Sentinel security audit: [015](015-sentinel-security-audit.md)
