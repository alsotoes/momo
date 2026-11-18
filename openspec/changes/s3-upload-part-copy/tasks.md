# Tasks

- [x] 1.1 Route the request to a handler instead of the 501 reject
- [x] 1.2 Share copy-source resolution with `CopyObject`
- [x] 1.3 Stream the source into the part spill, hashing as it goes
- [x] 1.4 Support `x-amz-copy-source-range` with S3's inclusive ends + 5-byte floor
- [x] 1.5 `416 InvalidRange` / `400 InvalidArgument` for bad ranges
- [x] 1.6 `404 NoSuchKey` (unwrapping wrapped not-found) / `404 NoSuchUpload`
- [x] 1.7 `<CopyPartResult>` with ETag + LastModified
- [x] 1.8 Tests: whole object, range, five bad ranges, unknown upload, missing source
- [x] 1.9 Real client: two ranged `upload-part-copy` calls assemble a 12 MiB object
- [x] 1.10 Docs: COMPATIBILITY, ARCHITECTURE, TESTING, unsupported-set note
