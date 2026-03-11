# Proposal: Implement End-to-End Encryption (E2EE)

- **Champion:** Gemini CLI
- **Status:** `Draft`

## 1. Problem

All network traffic in `momo`, including file metadata and content, is transmitted in plaintext over TCP connections. This is a critical security vulnerability. It exposes the data to eavesdropping and potential man-in-the-middle (MitM) attacks, making it unsuitable for any environment that is not physically secured.

## 2. Proposed Solution

Implement end-to-end encryption for all data transmitted between `momo` nodes. File content will be encrypted by the sender and decrypted only by the receiver, ensuring data confidentiality and integrity during transit.

- **Confidentiality:** Data will be unreadable to any party sniffing the network traffic.
- **Authentication:** The encryption scheme will also provide data authentication, preventing tampering.

## 3. Technical Spec

1.  **Adopt Go `crypto` Library:**
    - We will use the standard Go `crypto/aes` and `crypto/cipher` packages.
    - Specifically, we will use AES-GCM (Galois/Counter Mode), which is an Authenticated Encryption with Associated Data (AEAD) cipher that provides both confidentiality and authentication in one scheme.

2.  **Key Management:**
    - As a first step, a single, pre-shared symmetric key will be added to the `momo.conf` file. All nodes in the cluster will share this key.
    - **Future Improvement:** A more advanced key exchange mechanism (e.g., TLS, Noise Protocol) can be explored in a separate proposal.

3.  **Create a `crypto` Package:**
    - A new `src/crypto/` package will be created.
    - It will contain helper functions:
        - `Encrypt(plaintext []byte, key []byte) ([]byte, error)`
        - `Decrypt(ciphertext []byte, key []byte) ([]byte, error)`
    - It will also provide `cipher.Stream` wrappers for `io.Reader` and `io.Writer` to allow for efficient, on-the-fly encryption and decryption of file streams without requiring the entire file to be loaded into memory.

4.  **Integration with Transport Layer:**
    - The encryption/decryption logic will be integrated into the network communication layer.
    - When sending a file, the `io.Writer` for the network connection will be wrapped with an encrypting stream writer.
    - When receiving a file, the `io.Reader` for the network connection will be wrapped with a decrypting stream reader.

## 4. Performance Analysis & Justification

This is a classic security vs. performance trade-off.

-   **Expected Performance Impact:**
    1.  **CPU and Latency Penalty:** Encryption is a computationally intensive process. We expect a **measurable performance penalty** in terms of both CPU usage and `ns/op`. AES-GCM is highly optimized, especially on modern CPUs with AES-NI instruction sets, but the overhead will not be zero.

-   **Justification for Potential Penalties:** The performance cost is **absolutely necessary and justified**. Transmitting sensitive data in plaintext is not a viable option for any production-grade or security-conscious system. The cost of a data breach or unauthorized access far outweighs the computational cost of encryption. The goal of the implementation will be to minimize this overhead by using efficient streaming ciphers and best practices.

-   **Measurement Plan:**
    1.  **Baseline:** Establish a clear performance baseline using `make benchmark COUNT=10` on the current `master` branch.
    2.  **Post-Implementation:** Rerun the exact same benchmarks.
    3.  **Analysis:**
        - Carefully measure the percentage increase in `ns/op` and CPU usage for all benchmarked operations.
        - We must quantify the overhead precisely (e.g., "E2EE introduces a 12% latency penalty for file transfers").
        - The change is successful if the security goals are met and the performance overhead is within an acceptable, documented range. We will not block the change on performance degradation, but we must understand and accept the cost.

---
