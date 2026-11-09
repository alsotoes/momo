No blog post yet: this PR implements Phase 4 of `eliminate-singletons-v1`
(externalising network deadlines and the replication acceptance cap). The change is
operational configuration plumbing plus a defensive helper; it is not a complete
engineering narrative until the singleton phases (router injection, per-node
payload pool, encapsulated replication state) land. The final spec post will cover
the whole "no globals, no hardcoded operational limits" story, including this
phase; splitting a post for the config subset now would fragment it.
