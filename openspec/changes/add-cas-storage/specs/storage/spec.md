## ADDED Requirements

### Requirement: Content-Based File Addressing
The system SHALL store and retrieve files based on a cryptographic hash of their content.

#### Scenario: File Write with New Content
- **GIVEN** a client uploads a file that does not exist in the store
- **WHEN** the server receives the file
- **THEN** the server SHALL calculate the SHA-256 hash of the file's content and store the file in a path derived from this hash.

#### Scenario: File Write with Duplicate Content
- **GIVEN** a client uploads a file that already exists in the store (i.e., another file with the same content exists)
- **WHEN** the server receives the file
- **THEN** the server SHALL recognize that the content already exists and SHALL NOT write the new file to disk.
