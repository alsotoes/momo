# no-blog justification — 0075-dynamic-replication-order-v2 (metrics increment)

The narrative for this change is `docs/blog/posts/075-dynamic-replication-order.md`,
which explains the stigmergic reordering design end to end.

This increment only exposes the tracker's already-implemented state on the
existing `/metrics` scrape (`momo_replication_mode_score`,
`momo_replication_mode_samples`). It adds no new behaviour to explain and no
operator decision that the existing post does not already cover, so a second
post would duplicate the story rather than teach anything new.

no-blog: metrics surface for an already-published design; see post 075.
