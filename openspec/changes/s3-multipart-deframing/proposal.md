# Multipart Parts Are De-framed And Spill-backed

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1358

## Why

Modern S3 clients send every `UploadPart` body with `aws-chunked` framing plus a
checksum trailer. `handleUploadPart` read the raw body, so the framing (chunk
headers, terminating chunk, trailer block — 162 bytes for a 3-part 20 MiB upload)
was stored as part data. The assembled object was longer than the file and its
checksum could never validate:

```
aws s3 cp 20MiB.bin s3://b/big.bin     # appeared to succeed
head-object --key big.bin              # 20971682 bytes (file: 20971520)
aws s3 cp s3://b/big.bin out.bin       # AWS_ERROR_S3_RESPONSE_CHECKSUM_MISMATCH
```

The same code also held a whole part in RAM (`io.ReadAll`, 5 GiB cap) and copied
every part into one more buffer at completion, so a 5 GiB part meant ~10 GiB RSS.

Two decoder defects surfaced while fixing it: the aws-chunked reader assumed a
buffered source, and it demanded an extra CRLF after the terminating chunk even
when a trailer block followed.

## What Changes

- De-frame a part with the shared aws-chunked decoder when the request carries
  framing, bounded by the request's own content length so a part read cannot run
  into the next request on a keep-alive connection.
- Adopt the decoder's spill file as the part file (no second copy) and stream
  parts straight into the store at completion; part data is never held in memory.
- Remove an upload's spill directory on completion and on abort.
- The aws-chunked reader accepts any `io.Reader` and treats a trailer block as
  immediately following the terminating chunk, which is what S3 sends.
- Partial (206) responses no longer carry `x-amz-checksum-*`: a whole-object
  checksum cannot describe a range, and clients validate per range.

## Impact

- Affected specs: `s3-multipart`
- Affected code: `src/transport/s3_communicator.go`, `src/transport/aws_chunked.go`
- Memory: a multipart upload now costs disk spool space, not heap.
