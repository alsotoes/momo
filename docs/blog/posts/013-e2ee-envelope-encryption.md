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
  - 053-multitenancy-authorization-audit
---

The strongest security guarantee a cloud storage system can offer is simple: **even
if an attacker gains full root access to the storage nodes, the physical disks, and
the metadata databases, they cannot read a single byte of customer data.**

For a long time we did not have that guarantee. Encryption-at-rest protected the
disks, but the servers still held the keys, so a rogue operator or a compromised
daemon could read anything. We wanted a design where the server is structurally
incapable of reading the data it stores — not merely trusted not to. That is
**end-to-end encryption (E2EE)**: the data is encrypted on the client and stays
ciphertext everywhere else. momo implements it as **client-held envelope
encryption**, across both its S3 gateway and its native binary transport.

{{< diagram src="/diagrams/05-envelope-encryption.svg" alt="Envelope encryption overview" caption="Figure 1: High-level overview of client-held envelope encryption" >}}

---

## 1. The Key Hierarchy: KEK, CEK, and Envelopes

The obvious first idea is to encrypt every file directly with one long-lived key.
We rejected it for two concrete reasons:

- **Key wear-out.** AES-GCM requires that the same key never be reused with the
  same nonce. A single key encrypting terabytes of data will eventually repeat a
  nonce, and a nonce repeat is catastrophic — it can leak the XOR of two
  plaintexts. One key cannot safely cover an unbounded amount of data.
- **Rotation nightmares.** If that one key is ever compromised, re-securing
  petabytes means reading, decrypting, and rewriting every physical block on disk.
  A rotation that should be a metadata operation becomes a full data migration.

**Envelope encryption** solves both by splitting one key into two. The data itself
is encrypted with a short-lived key, and that short-lived key is encrypted with the
long-lived key. The long-lived key never touches the data directly, so it can be
rotated cheaply.

The three pieces:

1. **Content Encryption Key (CEK)** — also called a data key. A fresh, high-entropy
   256-bit AES key generated randomly in client RAM, used for exactly one object.
2. **Key Encryption Key (KEK)** — the client's long-lived master key. **The KEK
   never leaves the client device.** It is never transmitted across the network and
   never stored in server configuration.
3. **Wrapped CEK** — the client encrypts the 32-byte CEK under its KEK
   (`AES-256-GCM.Seal(KEK, CEK)`). The resulting small encrypted blob, the
   "envelope", travels alongside the ciphertext.

{{< diagram src="/diagrams/05a-key-hierarchy.svg" alt="Key hierarchy" caption="Figure 2: Cryptographic separation between Master KEK, Ephemeral CEK, and Wrapped CEK" >}}

Because the KEK only ever wraps a 32-byte key, rotating it means re-wrapping one
small envelope per object — constant work per object, regardless of how large the
object is. That is the whole point of the split.

---

## 2. The Ingest (PUT) Pipeline

During an upload, encryption happens entirely before any byte crosses the network
boundary. The client generates the CEK, wraps it, and streams the payload through
an encrypting reader:

{{< diagram src="/diagrams/05b-put-flow.svg" alt="E2EE PUT Ingest flow" caption="Figure 3: Upload flow — client encrypts payload and wraps key before network transit" >}}

The encryption helper in the crypto core looks like this:

```go
type Envelope struct {
    KeyID      string `json:"key_id"`
    WrappedCEK []byte `json:"wrapped_cek"`
    Nonce      []byte `json:"nonce"`
}

// ClientEncrypt streams plaintext into AES-256-GCM ciphertext
func (c *Client) EncryptStream(plaintext io.Reader, masterKEK []byte) (io.Reader, *Envelope, error) {
    // 1. Generate random 32-byte CEK — unique per object, never reused
    cek := make([]byte, 32)
    if _, err := rand.Read(cek); err != nil {
        return nil, nil, err
    }

    // 2. Wrap CEK with Master KEK — the KEK encrypts the data key, not the data
    wrappedCEK, nonce, err := sealKey(masterKEK, cek)
    if err != nil {
        return nil, nil, err
    }

    // 3. Construct streaming AES-GCM cipher over payload — constant memory
    //    regardless of object size
    cipherStream, err := newGCMEncryptReader(plaintext, cek)
    if err != nil {
        return nil, nil, err
    }

    env := &Envelope{KeyID: "default", WrappedCEK: wrappedCEK, Nonce: nonce}
    return cipherStream, env, nil
}
```

The server receives only the raw AES-GCM ciphertext stream and the JSON envelope.
It indexes the envelope in its metadata store and writes the ciphertext directly
into the content-addressed blob store. At no point does a plaintext byte or the
KEK reach the server.

---

## 3. The Retrieval (GET) Pipeline

When downloading, the process is inverted. The server can only ever hand back
ciphertext; the client does all the decryption:

{{< diagram src="/diagrams/05c-get-flow.svg" alt="E2EE GET Retrieval flow" caption="Figure 4: Download flow — server returns ciphertext, client decrypts with private KEK" >}}

1. The server streams the raw ciphertext blob from disk and includes the
   `WrappedCEK` envelope in the response headers.
2. The client receives the envelope, extracts the wrapped key, and unwraps it
   locally using its private KEK (`CEK = KEK.Open(WrappedCEK)`).
3. The client streams the ciphertext through an AES-GCM decrypting reader,
   verifying the authentication tag in real time as bytes arrive.

A tampered or truncated payload fails authentication before the client ever
consumes it, so the client never acts on unverified bytes.

---

## 4. Trade-off Analysis

Envelope encryption is not free. Here is what it buys and what it costs against
the alternative of server-side encryption, where the server holds the keys:

| Metric / Dimension | Server-Side Encryption (SSE-S3) | Client-Held Envelope Encryption (E2EE) |
|---|---|---|
| **Server trust assumption** | Server holds keys; a rogue admin can read data | **Zero trust**: server holds only ciphertext |
| **CAS deduplication** | Preserved (plaintext hashes match) | Neutralized (a random CEK produces unique ciphertext) |
| **Key rotation overhead** | Requires re-encrypting all stored blobs | **Constant time per object**: re-wrap only the 32-byte CEK |
| **Client CPU overhead** | Zero client CPU | Client performs AES-NI encryption/decryption |
| **Data loss risk on key loss** | Recoverable via server/cloud KMS | **Permanent**: a lost KEK means the data is irrecoverable |

The deduplication row is the one that hurts most. Because every object gets a
fresh random CEK, two identical files encrypt to two different ciphertexts, so the
store can no longer collapse them. We treat that as an acceptable price for true
zero trust, and we claw some of it back with a separate confidential-deduplication
protocol rather than by weakening the encryption.

---

## 5. Security Invariants (🛡 Sentinel Mindset)

In accordance with [docs/CORE/STANDARDS.md](../../STANDARDS.md):

- 🛡 **Sentinel (truncation defense).** Streaming AES-GCM carries an authenticated
  integrity footer. If an attacker truncates a file in transit or deletes trailing
  disk blocks, the client's GCM tag verification fails with `syscall.EBADMSG`,
  which prevents partial-read vulnerabilities where a client might act on a
  silently shortened object.
- 🛡 **Fail-closed key semantics.** If a client attempts to decrypt an object with
  an invalid KEK, decryption halts immediately. The server never attempts
  "fallback" plaintexts or heuristic recoveries — a failure to authenticate is
  always a hard stop, never a guess.

## References / Dig deeper

- Encryption implementation: `src/crypto/envelope.go`.
- Spec: `openspec/changes/add-e2e-encryption`.
- Pull requests: [#779](https://github.com/alsotoes/momo/pull/779),
  [#781](https://github.com/alsotoes/momo/pull/781).
- Sibling posts: [006: Pluggable Storage Backends](006-pluggable-storage-backends.md),
  [014: Confidential Dedup via OPRF](014-confidential-dedup-oprf.md),
  [015: The Sentinel Sweep](015-sentinel-security-audit.md).
