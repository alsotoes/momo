# Tasks: bolt-optimize-datestamp

## Implementation

- [x] Replace `time.Format("20060102")` with `amzDate[:8]` in `src/storage/s3_blobstore.go`.
- [x] Replace `time.Format("20060102")` with `amzDate[:8]` in `src/transport/external_client_test.go`.
- [x] Replace `time.Format("20060102")` with `amzDate[:8]` in `src/transport/s3_communicator_presigned_test.go`.
- [x] Replace `time.Format("20060102")` with `amzDate[:8]` in `src/transport/s3_communicator_test.go`.
- [x] Replace `time.Format("20060102")` with `amzDate[:8]` in `src/transport/sigv4_test.go`.
- [x] Replace `time.Format("20060102")` with `amzDate[:8]` in `src/transport/streaming_gateway_test.go`.

## Tests

- [x] Verify tests pass with `make test`.

## Docs

- [x] Create educational blog post for this optimization.
- [x] Generate ADR.
