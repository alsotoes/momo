No blog post: this change is an informational addition to `/health/detailed`
(learned replication mode, adaptive thresholds, election status, persistence flag)
plus a documentation section describing existing persistence behaviour. It makes
state that already existed observable; it introduces no new engineering decision,
mechanism, or trade-off to narrate. The engineering narrative for the underlying
state lives in posts 066 (adaptive thresholds), 067 (controller election), and 065
(metadata durability).
