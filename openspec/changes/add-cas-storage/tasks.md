## 1. Implementation
- [x] 1.1 Create the `src/storage` directory and define the `Store` interface.
- [x] 1.2 Implement the `CASStore` struct, including the content hashing and path transformation logic.
- [x] 1.3 Refactor `server/file.go` to use the new `storage.Store` for all file I/O operations.
- [x] 1.4 Update the wire protocol to send the file hash before the file content.
- [x] 1.5 Implement the deduplication check in the `Write` method of the `CASStore`.
- [x] 1.6 Write unit tests for the `storage` package.
- [x] 1.7 Run the performance and deduplication measurement plan as defined in the proposal.
