# Bolt Learnings and Prevention Strategies

This document tracks security vulnerabilities and performance anti-patterns identified in the Momo project that relate to performance optimizations, resource management, and low-level system interactions.

## 2024-05-09 - [Eliminate Heap Allocations for Fixed-Size Network Payloads]
**Learning:** When using `io.ReadFull` to read fixed-size data from a network connection into a buffer, passing a slice created with `make([]byte, ...)` forces that allocation to escape to the heap. This occurs because the interface method `Read(p []byte)` forces the argument to escape in Go's current escape analysis (unless it can prove otherwise, which is often not possible across interface boundaries).
**Action:** To eliminate this recurring heap allocation and garbage collection overhead during high-frequency network operations, explicitly declare a fixed-size array on the stack (e.g., `var buffer [192]byte`) and pass a slice of it (`buffer[:]`) to `io.ReadFull`. This ensures the array remains on the stack while satisfying the `io.Reader` interface. Note: due to the way `var buffer [N]byte` requires a constant for N, ensure `N` is composed of constants. This optimization is particularly beneficial in hot paths like `getMetadata`.

## 2024-05-11 - Do not replace PadString with zero-initialized arrays
**Learning:** While replacing `make([]byte, ...)` with stack allocated arrays (`var buffer [...]byte`) to avoid heap escapes, do not replace the explicit `PadString` method call with a direct `copy()` into a zero-initialized array to try to save the formatting memory allocation overhead. Although `copy()` null-pads correctly, it lacks the logic to truncate the string if the string exceeds the buffer size, which `PadString` safely handles. Automated reviews flag this explicitly as a critical regression.
**Action:** When replacing dynamically allocated byte slices with stack-allocated byte arrays, preserve the `PadString` usage and just copy its output into the new stack array.

## 2026-05-20 - [Optimize Handshake Reads via Stack Buffer]
**Learning:** Performing multiple sequential `io.ReadFull` calls for small, fixed-length protocol headers (like AuthToken and Timestamp) incurs unnecessary system call overhead on every connection.
**Action:** Always combine sequential reads of fixed-size network payloads into a single stack-allocated byte array of their total length and use a single `io.ReadFull` call to reduce system calls and improve performance without causing heap escapes.

## 2026-05-21 - Consolidate Network Writes and Reduce Allocations in Replication Metrics and Server
**Learning:** In Go, repeatedly calling `conn.Write` or using `json.NewEncoder` for small payloads (like authentication tokens + JSON structs) causes unnecessary memory allocations and system call overhead.
**Action:** Consolidate network writes by marshalling JSON first and appending it to a dynamically-sized buffer using `make` and `append`, then sending the unified byte slice via a single `conn.Write` call. This prevents string-to-byte allocation of JSON, the encoder's intermediate buffer allocation, and halves the number of write system calls while maintaining memory safety.

## 2026-05-30 - Eliminate Heap Allocation in Cryptographic Hash Generation
**Learning:** Calling `hash.Sum(nil)` on a cryptographic hash interface forces a heap allocation for the resulting byte slice, which increases memory overhead and garbage collection pressure, particularly during intensive operations like hashing files and stream data in hot paths.
**Action:** Always pre-allocate a fixed-size byte array on the stack (e.g., `var buf [sha256.Size]byte`) and use a slice of it `buf[:0]` when invoking `hash.Sum()` to eliminate the heap allocation and associated garbage collection overhead.
