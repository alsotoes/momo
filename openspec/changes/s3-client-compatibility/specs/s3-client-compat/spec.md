# S3 Client Compatibility

## Purpose

Prove the S3 gateway works for a standard S3 client, not only for requests
written against our own assumptions.

## Requirements

### Requirement: Round-Trip With A Standard Client

CI SHALL drive a multi-node cluster with aws-cli and verify that an object a
client writes can be read back byte-for-byte.

#### Scenario: Put, head, get

- **WHEN** aws-cli uploads an object
- **THEN** `head-object` succeeds and `get-object` returns identical bytes

#### Scenario: Key-only listing

- **WHEN** aws-cli lists the bucket
- **THEN** the reported key carries no bucket prefix

#### Scenario: Large object

- **WHEN** aws-cli uploads a 20 MiB object in one part and downloads it again
- **THEN** the bytes are identical

Multipart uploads are deliberately out of scope here: modern clients send each
part aws-chunked and the gateway stores the framing, so the object is longer than
the file (issue #1358). The script prints a note pointing at that issue instead
of pretending the case is covered.

#### Scenario: Copy and delete

- **WHEN** aws-cli copies an object and then deletes it
- **THEN** the copy is readable and the deleted key returns 404

### Requirement: Skips Without The Client

The check SHALL skip cleanly when aws-cli is unavailable instead of failing a
local run.

#### Scenario: aws-cli absent

- **WHEN** the script runs on a host without aws-cli
- **THEN** it prints a skip notice and exits successfully
