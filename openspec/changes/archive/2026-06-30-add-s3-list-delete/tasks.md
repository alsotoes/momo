## 1. Storage and Database Updates

- [x] 1.1 Add `List() ([]common.FileMetadata, error)` to the `Store` interface in `src/storage/storage.go`
- [x] 1.2 Implement the `List()` method in `CASStore` in `src/storage/storage.go` using Bbolt's cursor to scan buckets
- [x] 1.3 Add a unit test in `src/storage/storage_test.go` to verify the listing of existing files

## 2. S3 Communicator Enhancements

- [x] 2.1 Add `ErrRequestHandled` to the transport layer in `src/transport/communicator.go`
- [x] 2.2 Add a `store` field and `SetStore(store storage.Store)` method to `S3Communicator` in `src/transport/s3_communicator.go`
- [x] 2.3 Implement S3 URL/bucket/key parsing helper `extractS3BucketAndKey` in `src/transport/s3_communicator.go`
- [x] 2.4 Implement high-performance XML formatter `FormatListObjectsV2XML` in `src/transport/s3_communicator.go`
- [x] 2.5 Update `HandshakeServer` to intercept S3 `GET` (List), `GET` (GetObject), and `DELETE` requests

## 3. Server Integration

- [x] 3.1 Update `Daemon` in `src/server/server.go` to check and inject the storage engine into the accepted `Communicator`
- [x] 3.2 Update `Daemon` to gracefully catch and process the `transport.ErrRequestHandled` sentinel error

## 4. Testing and Validation

- [x] 4.1 Add comprehensive unit tests in `src/transport/s3_communicator_test.go` for the XML formatter, URL parsing, ListObjectsV2, GetObject, and DeleteObject
- [x] 4.2 Run the entire test suite and verify linter, type-check, and code coverage
