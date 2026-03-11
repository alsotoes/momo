## ADDED Requirements

### Requirement: Data Confidentiality
The system SHALL encrypt all file content during network transmission to prevent eavesdropping.

#### Scenario: File Transfer
- **GIVEN** a node is configured with a shared secret key
- **WHEN** the node sends a file to a peer
- **THEN** the file content SHALL be encrypted using AES-GCM before being sent over the network.

#### Scenario: File Reception
- **GIVEN** a node is configured with a shared secret key
- **WHEN** the node receives an encrypted file from a peer
- **THEN** the node SHALL decrypt the file content using AES-GCM before writing it to storage.
