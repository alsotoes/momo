## ADDED Requirements

### Requirement: User-Defined Path Storage & Retrieve

Momo MUST support storing and retrieving a user-defined virtual folder/directory path alongside standard file metadata, without altering the physical CAS store layout.

#### Scenario: Metadata Path Validation & Retrieval
- **Given** a Momo storage node with an initialized Bbolt metadata index.
- **When** the server receives an upload payload with `Name="invoice.pdf"`, `Hash="ab12hash"`, and `RemotePath="customer01/billing"`.
- **Then** the metadata index MUST store the mapping of `RemotePath` to the content hash.
- **And** subsequent queries for the metadata of `"invoice.pdf"` MUST return the exact `RemotePath` value `"customer01/billing"`.

### Requirement: Virtual Path Normalization & Sanitization

Momo MUST normalize and sanitize all virtual paths before storing them in the metadata index to prevent duplicate records, whitespace inconsistencies, or path-delimiter variations.

#### Scenario: Normalizing Slashes and Whitespace
- **Given** a Momo server metadata validator.
- **When** the server receives an upload request with a raw virtual path `/customer01//documents/invoice.pdf/ `.
- **Then** the path MUST be normalized to `customer01/documents/invoice.pdf` before indexing.

### Requirement: Conflict Resolution & Overwrite

The Bbolt metadata index MUST safely handle situations where a new upload request targets an existing, already-indexed virtual path.

#### Scenario: Overwriting an Existing Virtual Path
- **Given** a virtual path `customer01/file.txt` pointing to CAS hash `hash01`.
- **When** the client uploads a new file with the same path `customer01/file.txt` but different content hash `hash02` under an overwrite policy.
- **Then** the metadata index MUST update the pointer for `customer01/file.txt` to point to `hash02`.
- **And** the reference count for `hash01` MUST be decremented accordingly.