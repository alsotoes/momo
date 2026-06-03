## ADDED Requirements
### Requirement: Defensive Parsing Validation
All data parsed from external sources (network connections, configuration files) SHALL undergo strict boundary and format validation prior to type conversion or application logic execution.

#### Scenario: Malformed Replication Mode Packet
- **GIVEN** an active Momo daemon listening for client connections
- **WHEN** a client sends a handshake packet where the replication mode byte is non-numeric (e.g., 'X' or a control character)
- **THEN** the parsing logic (`strconv.Atoi` or similar) must not panic
- **AND** the daemon must gracefully log an `AUDIT` warning and terminate the connection
- **AND** the daemon must continue accepting new, valid connections

### Requirement: Bounded Resource Allocation
The system SHALL NOT allocate memory or disk buffers based solely on unvalidated sizes provided in network metadata.

#### Scenario: Malicious File Size Header
- **GIVEN** a client initiating a file transfer
- **WHEN** the client provides a metadata header indicating a file size exceeding `MaxFileSize` (1GB) or a negative value
- **THEN** the server must immediately reject the transfer and close the connection
- **AND** no large byte slices or pre-allocated disk spaces should be created.

### Requirement: Fuzz-Tested Resilience
All critical parsing functions exposed to network data SHALL be verified using Go's native fuzzing framework (`testing.F`) to ensure immunity against panics from unexpected byte combinations.

#### Scenario: Continuous Fuzzing Pipeline
- **WHEN** the CI/CD pipeline executes the test suite
- **THEN** fuzz tests targeting `parsePaddedIntFast` and metadata extraction must run
- **AND** they must prove that no combination of bytes can trigger an out-of-bounds panic or runtime crash.

### Requirement: Goroutine Panic Recovery
Critical long-running loops and dynamically spawned network handlers SHALL implement panic recovery to prevent total application failure.

#### Scenario: Unexpected runtime panic in connection handler
- **WHEN** a previously unknown bug causes a panic during the processing of a specific file
- **THEN** the `defer recover()` block in the connection handler must catch the panic
- **AND** log a CRITICAL error with the stack trace
- **AND** safely close the connection, allowing the main daemon to continue operating.

### Requirement: Strict Performance Threshold
The addition of defensive checks and panic recovery mechanisms SHALL NOT incur a performance penalty greater than 5% as measured by the `make benchmark` suite.

#### Scenario: Performance regression check
- **WHEN** a PR implementing Zero-Crash Hardening is submitted
- **THEN** the automated benchmarks must confirm the degradation is within the 5% limit.
