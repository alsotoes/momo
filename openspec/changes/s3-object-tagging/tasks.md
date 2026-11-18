# Tasks

- [x] 1.1 `tagSet` model with encode/decode in the S3 `x-amz-tagging` wire form
- [x] 1.2 Validation: 10 tags max, key 1–128, value ≤256, unique keys, no controls
- [x] 1.3 `GET`/`PUT`/`DELETE ?tagging` handlers with S3 error mapping
- [x] 1.4 Store in the S3 metadata sidecar, preserving other headers
- [x] 1.5 `x-amz-tagging` accepted on a plain PUT
- [x] 1.6 Removed from the honest-501 object set (bucket-root tagging stays 501)
- [x] 1.7 Tests: round-trip, empty set, delete, unknown object, six invalid shapes,
      encoding determinism/reversibility
- [x] 1.8 Live client: aws-cli put/get/delete, 11-tag rejection, object still reads
- [x] 1.9 Docs: COMPATIBILITY, ARCHITECTURE, TESTING
