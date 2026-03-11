# Proposal: Implement Content-Addressable Storage (CAS)

- **Champion:** Gemini CLI
- **Status:** `Draft`

## 1. Problem

Currently, `momo` stores and identifies files based on their user-provided filenames. This has two main drawbacks:
1.  **No Data Deduplication:** If the same file is uploaded multiple times with different names, it will be stored multiple times, consuming unnecessary disk space and network bandwidth during replication.
2.  **Integrity Post-Verification:** Data integrity (via SHA-256 hashing) is checked *after* the entire file has been transferred and written to disk. The identity of the file is not intrinsically linked to its content, making the system less robust.

## 2. Proposed Solution

Refactor the storage mechanism to use a Content-Addressable Storage (CAS) model. In a CAS system, a file is identified by a cryptographic hash of its content (e.g., SHA-256).

- **Intrinsic Integrity:** Files are stored in a path derived from their hash. This guarantees that if the content changes, the path changes, and it's treated as a new file.
- **Automatic Deduplication:** When a file is to be stored, the server will first calculate its hash. If a file with that hash already exists, the new upload can be discarded, saving disk space and preventing redundant replication over the network.

## 3. Technical Spec

1.  **Create a `storage` Package:**
    - Create a new `src/storage/` package to abstract all file system interactions.
    - Define a `storage.Store` interface with methods like `Write(key string, content io.Reader)`, `Read(key string)`, `Has(key string)`.

2.  **Implement `CASStore`:**
    - Create a `storage.CASStore` struct that implements the `Store` interface.
    - It will use a `PathTransformFunc` (similar to `distributedfilesystemgo`) to convert a file's SHA-256 hash into a structured directory path (e.g., hash `abcdef123...` becomes `/ab/cd/ef/abcdef123...`). This prevents having too many files in a single directory, which is inefficient.
    - The `Write` method will first buffer the incoming stream to calculate its SHA-256 hash. It will then check if an object with that hash already exists. If so, it returns immediately. If not, it saves the file to the hash-derived path.

3.  **Refactor `server/file.go`:**
    - The `getFile` function will be updated to use the new `storage.Store`.
    - The wire protocol will be updated. The client will send the file's content hash *before* the file stream. The server will use this hash to check for existence before reading the stream.

## 4. Performance Analysis & Justification

This change introduces a trade-off between CPU usage and I/O efficiency.

-   **Expected Performance Impact:**
    1.  **CPU Overhead:** There will be a **minor performance penalty** in terms of CPU usage. The system will need to hash file content *before* deciding to write it to disk.
    2.  **I/O Improvement:** In workloads with duplicate files, there will be a **significant performance improvement**. The system will avoid expensive disk writes and network replication for redundant data. Read performance may also improve slightly due to a more balanced directory structure.

-   **Justification for Potential Penalties:** The CPU overhead of SHA-256 hashing is a well-understood and acceptable cost in modern systems. The architectural benefits—guaranteed data integrity and automatic deduplication—are substantial and align with the best practices for distributed storage systems. The performance penalty is justified by the significant gains in storage efficiency and reliability.

-   **Measurement Plan:**
    1.  **Baseline:** Establish a baseline using the existing `make benchmark` command.
    2.  **Post-Implementation:** Rerun the benchmarks. We will analyze the `ns/op` and CPU usage.
    3.  **New Benchmark Scenario:** Create a new E2E test that uploads the same large file 10 times with different names.
    4.  **Analysis:**
        - We expect a small increase in `ns/op` for single-file uploads.
        - The new E2E test should demonstrate a dramatic reduction in total time, disk usage, and network traffic compared to the baseline.
        - The change is successful if the single-file overhead is minimal (e.g., <5%) and the deduplication benefits are clearly demonstrated.

---
