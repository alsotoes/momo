# 0108-s3-multipart-deframing

## Status
Accepted

## Confidence
High

## Context
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

## Decision
- De-frame Part Bodies: An `UploadPart` whose body carries aws-chunked framing SHALL be stored as its decoded bytes.
- Parts Are Spill-backed: A part SHALL be written to disk rather than accumulated in memory.
- No Whole-object Checksum On A Range: A `206 Partial Content` response SHALL NOT carry an `x-amz-checksum-*` header.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: 

## References
- Issue: #1358
- PR: 
- Spec: `openspec/changes/s3-multipart-deframing/`
- Blog: 

