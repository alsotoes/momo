# s3-list-delete-get Specification

## Purpose
TBD - created by archiving change add-s3-list-delete. Update Purpose after archive.
## Requirements
### Requirement: S3 ListObjectsV2 API
The system SHALL support S3-compatible `ListObjectsV2` requests to list files in the storage. It MUST parse Prefix, Delimiter, and MaxKeys query parameters, group subdirectories under `<CommonPrefixes>`, and return standard-compliant XML.

#### Scenario: Listing files in the root bucket directory
- **WHEN** an S3 client makes a GET request to "/" with query "list-type=2"
- **THEN** the system returns a 200 OK with S3-compliant ListObjectsV2 XML listing all files in the system

#### Scenario: Listing files with prefix and delimiter grouping
- **WHEN** an S3 client makes a GET request to "/" with query "list-type=2&prefix=docs/&delimiter=/"
- **THEN** the system groups subdirectory elements under CommonPrefixes and returns only files matching the prefix directly

### Requirement: S3 DeleteObject API
The system SHALL support S3-compatible DELETE requests to delete a specific file. The server MUST remove the file's metadata index from BoltDB and return a 204 No Content response.

#### Scenario: Deleting an existing object
- **WHEN** an S3 client sends a DELETE request to "/documents/reports.pdf"
- **THEN** the system removes "/documents/reports.pdf" from BoltDB and returns a 204 No Content response

### Requirement: S3 GetObject API
The system SHALL support S3-compatible GET requests to retrieve a specific file. The server MUST look up the file in BoltDB, load the blob, stream the contents back with a 200 OK response, or return 404 Not Found if the file does not exist.

#### Scenario: Retrieving an existing object
- **WHEN** an S3 client sends a GET request to "/images/pic.png"
- **THEN** the system returns a 200 OK response and streams the binary content of the file

#### Scenario: Retrieving a nonexistent object
- **WHEN** an S3 client sends a GET request to "/missing.txt"
- **THEN** the system returns a 404 Not Found response

