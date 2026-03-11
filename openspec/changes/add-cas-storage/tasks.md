## 1. Implementation
- [ ] 1.1 Create the `src/storage` directory and define the `Store` interface.
- [ ] 1.2 Implement the `CASStore` struct, including the content hashing and path transformation logic.
- [ ] 1.3 Refactor `server/file.go` to use the new `storage.Store` for all file I/O operations.
- [ ] 1.4 Update the wire protocol to send the file hash before the file content.
- [ ] 1.5 Implement the deduplication check in the `Write` method of the `CASStore`.
- [ ] 1.6 Write unit tests for the `storage` package.
- [ ] 1.7 Run the performance and deduplication measurement plan as defined in the proposal.
