## ADDED Requirements

### Requirement: User-Defined Path Storage & Retrieve

Momo MUST support storing and retrieving a user-defined virtual folder/directory path alongside standard file metadata, without altering the physical CAS store layout.

#### Scenario: Metadata Path Validation & Retrieval
- **Given** a Momo storage node with an initialized Bbolt metadata index.
- **When** the server receives an upload payload with `Name="invoice.pdf"`, `Hash="ab12hash"`, and `RemotePath="customer01/billing"`.
- **Then** the metadata index MUST store the mapping of `RemotePath` to the content hash.
- **And** subsequent queries for the metadata of `"invoice.pdf"` MUST return the exact `RemotePath` value `"customer01/billing"`.