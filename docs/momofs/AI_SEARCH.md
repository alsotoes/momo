# AI-Ready Architecture & Fast Search

## 7. AI-Ready Architecture

### 7.1 Vector Embeddings

- **On-write**: Optionally compute vector embedding for text/image/audio content on ingest
- **Storage**: New BoltDB bucket `embeddings` — key: content hash, value: float32 vector (dimension configurable per tenant)
- **Index**: Approximate Nearest Neighbor (ANN) index — HNSW or IVF for semantic search
- **Use cases**: Semantic deduplication (detect similar content), content classification, intelligent tiering

### 7.2 Content Classification

- **On-write**: Classify content type (document, image, video, log, PII detected)
- **PII detection**: Flag objects containing personally identifiable information for GDPR review
- **Storage**: Classification metadata in `objects` bucket (extend ObjectMeta or new `classifications` bucket)

### 7.3 Intelligent Tiering

- **Access pattern tracking**: Per-object access frequency, last access time, size
- **ML model**: Predict hot/cold/warm classification
- **Auto-tiering**: Scrub thread moves objects between tiers based on ML predictions
- **Storage**: Access pattern stats in new `access_stats` bucket

### 7.4 BoltDB Schema Additions

| Bucket | Key | Value | Purpose |
|--------|-----|-------|---------|
| `embeddings` | content hash | float32[] (binary) | Vector embeddings for semantic search |
| `classifications` | content hash | JSON classification | Content type, PII flags, custom labels |
| `access_stats` | content hash | `{AccessCount, LastAccess, AvgLatency}` | Access pattern tracking for tiering |
| `ann_index` | node ID | HNSW graph snapshot | Distributed ANN index shards |

---

## 8. Fast Search

### 8.1 Current State

- `List()` returns all local files — no filtering, no search, no pagination
- No content search, no metadata search, no fuzzy search

### 8.2 Target: Multi-Modal Search

#### Metadata Search
- **Inverted index**: Built on BoltDB `namespace` and `paths` buckets
- **Query**: `List(prefix="/tenant/photos/", modified_after=2024-01-01, size_gt=1MB)`
- **Implementation**: BoltDB cursor scan with filter predicates (no external index needed for metadata)

#### Content Search
- **Full-text**: Inverted index over extracted text (documents, logs)
- **Bloom filters**: Per-bucket bloom filter for fast `Has()` across content hashes
- **Range queries**: BoltDB supports sorted key iteration natively

#### Semantic Search (AI)
- **Vector similarity**: HNSW index over embeddings — `Search(query="sunset photo", k=10)`
- **Hybrid**: Combine metadata filters + semantic similarity — `Search(prefix="/photos/", semantic="sunset", k=10)`

#### Search Architecture

```
┌──────────────────────────────────────────────────┐
│                  Search API                       │
├──────────┬──────────┬──────────┬────────────────┤
│ Metadata │ Full-Text │ Semantic │   Hybrid       │
│ (BoltDB) │ (Inverted)│ (HNSW)   │ (Combined)    │
│ cursor   │ index     │ index    │ filter+rank   │
│ scan     │ shards    │ shards   │               │
└──────────┴──────────┴──────────┴────────────────┘
         │           │          │
         ▼           ▼          ▼
    Local only   Distributed  Distributed
                 (scatter-    (scatter-
                  gather)      gather)
```
