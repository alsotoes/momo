# Tasks

- [x] 1.1 Add `.github/scripts/test-s3-client-compat.sh` (3-node cluster, aws-cli)
- [x] 1.2 Cover put/head/get with a byte comparison
- [x] 1.3 Cover key-only listing
- [x] 1.4 Cover copy and delete
- [x] 1.5 Cover a 20 MiB single-part round-trip (multipart tracked in #1358)
- [x] 1.6 Skip cleanly when aws-cli is missing
- [x] 1.7 Add `.github/workflows/s3_client_compat.yml`
- [x] 1.8 Add the `make smoke-s3-client` target
- [x] 1.9 Document the check in `TESTING.md`
- [x] 1.10 Verify the script passes locally against a real cluster
