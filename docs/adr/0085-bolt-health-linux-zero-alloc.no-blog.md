# No blog post: bolt-health-linux-zero-alloc

This is a micro-optimization of an internal sampler: the health monitor's
`/proc` parsing drops from 5/24 allocations per sample to zero, saving roughly
0.8 µs per tick on a 10-second timer. There is no user-visible behaviour, no
protocol surface, and no architectural decision a reader would learn from beyond
"scan bytes instead of strings", which the `⚡ Bolt` learning log already records.

Blog coverage is therefore deliberately skipped under Rule 76.
