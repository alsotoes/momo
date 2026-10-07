---
title: "S3 501 Discipline: Honest 'Not Implemented' for 16 Bucket Config Subresources"
date: 2026-08-24T20:00:50Z
draft: false
post_type: issue
tags: [s3, compatibility, sentinel]
categories: [s3]
summary: "16 bucket config subresources (versioning, ACL, policy, CORS, lifecycle, etc.) now return honest 501 NotImplemented instead of silently misrouting to ListObjects/GetObject."
artifacts:
  - {type: spec, path: openspec/changes/s3-p3-bucket-subresource-501}
  - {type: issue, id: "912"}
related:
  - 008-s3-gateway-core
  - 009-s3-multipart-and-breadth
  - 011-s3-https-tls-enforcement
  - 034-s3-501-discipline-object-subresources
  - 035-s3-501-discipline-remaining-ops
---
S3 is a large API, and momo implements the part of it a storage backend actually
needs. The rest has to fail in a way a client can understand. For a long time,
"the rest" failed in a way no client could: a request for a feature we do not
support was quietly answered as if it were a different request. This post is about
the first batch of those we fixed — sixteen bucket configuration operations that
now return an honest "not implemented" instead of the wrong data.

## The Real Problem

Some S3 operations are not addressed by the URL path alone. A `GET` on a bucket
is normally "list the objects"; add a query parameter such as `?versioning` and it
becomes "read the bucket's versioning configuration". A query parameter that
selects an operation like this is called a **subresource**.

momo's gateway did not implement sixteen of these bucket-level configuration
subresources — versioning, access control lists, bucket policies, CORS, lifecycle
rules, and so on. The trouble was not that they were unsupported. It was that the
router, seeing no code to handle them, fell through to the default bucket
handlers: it answered a versioning request with an object listing, or ignored the
parameter and returned the current object. A client asked one question and got an
answer to a different one — the most dangerous kind of bug, because nothing looks
broken.

## Why the Obvious Fixes Failed

**"Implement them."** Versioning, policies, and object lock are large features
with real semantics. We will implement some of them eventually, but we needed them
to fail honestly *now*, not silently misbehave until then.

**"Return 404 Not Found."** That is a lie in the other direction: the route
exists, and the server does understand what was asked. It just does not support
it. A wrong status sends clients down the wrong error-handling path.

**"Let the fall-through stand."** This is what we had, and it is how a client ends
up treating a bucket listing as a versioning configuration.

## The Solution: Explicit, Centralized Rejection

We added one place where unsupported bucket configuration subresources are
recognized and rejected, checked immediately after the request's bucket name and
key have been parsed. When the request targets the bucket root (no object key) and
names one of the sixteen subresources, every HTTP method gets the same answer:
**501 Not Implemented**.

`501` is the honest status here — the server recognizes the operation and does not
support it, which is distinct from `404` (no such thing) and from `200` (here is
your data).

The sixteen subresources now rejected:

| Subresource | Meaning |
|-------------|---------|
| `versioning` | Bucket versioning configuration |
| `versions` | List all object versions |
| `acl` | Access control list |
| `policy` | Bucket policy |
| `cors` | Cross-origin resource sharing rules |
| `website` | Static website hosting |
| `lifecycle` | Lifecycle/expiry rules |
| `tagging` | Bucket tags |
| `encryption` | Default encryption configuration |
| `publicAccessBlock` | Public access block settings |
| `accelerate` | Transfer acceleration |
| `replication` | Cross-region replication |
| `requestPayment` | Request payer setting |
| `logging` | Access logging |
| `objectLock` | Object lock configuration |
| `notification` | Event notification configuration |

## Guarding What We *Do* Support

The rejection has to be precise. Several bucket-level subresources are real
features here, and they must keep working:

| Subresource | Status |
|-------------|--------|
| `location` | Supported — returns the bucket's region |
| `list-type` | Supported — object listing |
| `uploads` | Supported — list multipart uploads |
| `uploadId` / `partNumber` | Supported — multipart operations |
| `delete` | Supported — batch delete |

## How We Verified

- Nine unsupported subresources all return `501 Not Implemented`.
- `location` still returns `200 OK` with the region.
- `list-type=2` still returns `200 OK` with the object listing.
- An object-level tag request sent to the bucket root correctly returns `404`,
  because tagging is only valid on objects.
- Multipart routing (`uploads`, `uploadId`) is untouched.

## Failure Modes

| Risk | Guard |
|------|-------|
| Rejecting a subresource we actually support | The rejection list is an explicit set; supported ones are excluded and covered by tests |
| A future subresource silently falling through again | New unsupported operations must be added to the same centralized list |
| An over-large error response | The `501` body is written with a bounded writer |

## When NOT to Use This

- **When you intend to implement the feature.** The rejection list is for
  genuinely unsupported operations, not a parking lot for half-built ones.
- **On paths where the subresource is meaningful but different.** Object-level
  tagging is valid on an object key, so the bucket-level rejection must not catch
  it — which is why the check is scoped to the bucket root.

## Engineering Standards (🛡 Sentinel)

Per [docs/CORE/STANDARDS.md](../../STANDARDS.md): 🛡 **Sentinel** — honest error
semantics and fail-closed behavior. Unsupported means `501`, never a wrong `200`
or a misleading `404`.

## References / Dig deeper

- Spec: `openspec/changes/s3-p3-bucket-subresource-501/`.
- Issue: #912.
- Related posts: [008: S3 Gateway Core](008-s3-gateway-core.md),
  [009: S3 Multipart and Breadth](009-s3-multipart-and-breadth.md),
  [011: S3 HTTPS/TLS Enforcement](011-s3-https-tls-enforcement.md),
  [034: Object-Level Subresources](034-s3-501-discipline-object-subresources.md),
  [035: Remaining Operations](035-s3-501-discipline-remaining-ops.md).
