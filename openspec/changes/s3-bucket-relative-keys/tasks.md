# Tasks

## 1. Naming

- [x] 1.1 Store the bucket-relative key when the path has one
- [x] 1.2 Keep legacy flat semantics for a top-level path
- [x] 1.3 Apply the same rule in the `OPTIONS`-then-`PUT` fallback
- [x] 1.4 Replace the empty-name sentinel with `metaParsed`

## 2. Verification

- [x] 2.1 `aws-cli`: `put-object` → `head-object` → `get-object` round-trip, bytes match
- [x] 2.2 `aws-cli`: `list-objects-v2` reports the key only
- [x] 2.3 `aws-cli`: `s3 cp` of a 20 MB object (multipart) round-trips
- [x] 2.4 `aws-cli`: `copy-object` and `delete-object`
- [x] 2.5 Regression tests: bucket-relative name, flat top-level name, deep-path round-trip
- [x] 2.6 Full `-race` transport/server suites, `make lint`, `gofmt`

## 3. Docs

- [x] 3.1 Document bucket-relative naming and the implicit-bucket caveat
- [x] 3.2 Add the client-compatibility check to `TESTING.md`
