# Research Papers for Momo

Generated from a codebase and documentation review on 2026-08-04.

## Project Map

Momo is a Go distributed object storage system. The current code and docs point to these research areas:

| Momo area | Code and docs | Research themes |
|---|---|---|
| Algorithmic object placement | `src/common/crush.go`, `docs/CRUSH.md` | CRUSH, rendezvous hashing, consistent hashing, decentralized placement |
| Object storage engine | `src/storage/`, `docs/ARCHITECTURE.md`, `docs/momofs/CURRENT_ARCHITECTURE.md` | Content-addressable storage, deduplication, local storage backend design |
| Replication modes | `src/server/replication.go`, `src/client/client.go`, `docs/REPLICATION_STRATEGIES.md` | Chain replication, fan-out replication, quorum and consistency trade-offs |
| Adaptive replication | `src/metrics/`, `docs/POLYMORPHIC_SYSTEM.md` | Self-adaptive systems, autonomic control loops, runtime reconfiguration |
| P2P coordination | `src/p2p/`, `docs/P2P.md` | Gossip, SWIM membership, failure detectors, scatter-gather, leases |
| Transport protocols | `src/transport/`, `docs/PROTOCOL.md` | TCP framing, QUIC, S3-compatible REST gateways |
| Secure E2EE and confidential deduplication | `src/crypto/`, `openspec/changes/secure-e2ee-confidential-dedup/` | Message-locked encryption, OPRF/VOPRF, Shamir secret sharing, HKDF, AES-GCM nonce safety |
| Future MomoFS roadmap | `docs/momofs/*.md` | Distributed metadata, self-healing, multi-tenancy, GDPR, fast recovery, AI search, erasure coding, HPC/cloud readiness |

## Read First

Read these in order if the goal is to understand the project quickly:

1. **Ceph: A Scalable, High-Performance Distributed File System** - Sage A. Weil, Scott A. Brandt, Ethan L. Miller, Darrell D. E. Long, Carlos Maltzahn, 2006. OpenAlex citations: 1430. PDF: <https://escholarship.org/uc/item/1pf8q17j>
2. **CRUSH: Controlled, Scalable, Decentralized Placement of Replicated Data** - Sage A. Weil, Scott A. Brandt, Ethan L. Miller, Carlos Maltzahn, 2006. PDF: <https://ceph.io/assets/pdfs/weil-crush-sc06.pdf>
3. **RADOS: A Scalable, Reliable Storage Service for Petabyte-scale Storage Clusters** - Sage A. Weil, Andrew W. Leung, Scott A. Brandt, Carlos Maltzahn, 2007. PDF: <https://ceph.io/assets/pdfs/weil-rados-pdsw07.pdf>
4. **Chain Replication for Supporting High Throughput and Availability** - Robbert van Renesse, Fred B. Schneider, 2004. PDF: <https://www.cs.cornell.edu/fbs/publications/ChainReplicOSDI.pdf>
5. **Dynamo: Amazon's Highly Available Key-value Store** - Giuseppe DeCandia et al., 2007. PDF: <https://www.allthingsdistributed.com/files/amazon-dynamo-sosp2007.pdf>
6. **SWIM: Scalable Weakly-consistent Infection-style Process Group Membership Protocol** - Abhinandan Das, Indranil Gupta, Ashish Motivala, 2002. PDF: <https://www.cs.cornell.edu/projects/Quicksilver/public_pdfs/SWIM.pdf>
7. **The QUIC Transport Protocol: Design and Internet-Scale Deployment** - Adam Langley et al., 2017. OpenAlex citations: 799. DOI/PDF: <https://dl.acm.org/doi/pdf/10.1145/3098822.3098842>
8. **Venti: A New Approach to Archival Storage** - Sean Quinlan, Sean Dorward, 2002. PDF: <https://www.usenix.org/legacy/event/fast02/quinlan/quinlan.pdf>
9. **Leases: An Efficient Fault-Tolerant Mechanism for Distributed File Cache Consistency** - Cary Gray, David Cheriton, 1989. OpenAlex citations: 72. DOI/PDF: <https://dl.acm.org/doi/pdf/10.1145/74851.74870>
10. **Software Engineering for Self-Adaptive Systems: A Second Research Roadmap** - Rogério de Lemos, Holger Giese, Hausi Muller, et al., 2013. OpenAlex citations: 718. PDF: <http://repository.icesi.edu.co/bitstreams/1ebf94de-a1b3-4064-a2a2-070e82453dde/download>
11. **DupLESS: Server-Aided Encryption for Deduplicated Storage** - Mihir Bellare, Sriram Keelveedhi, Thomas Ristenpart, 2013. PDF: <https://www.usenix.org/system/files/conference/usenixsecurity13/sec13-paper_bellare.pdf>

## Object Storage and Placement

| Paper | Why it matters for Momo |
|---|---|
| **Ceph: A Scalable, High-Performance Distributed File System** | Best single overview for Momo's decentralized-storage lineage: separation of data and metadata, CRUSH placement, object storage, and recovery. |
| **CRUSH: Controlled, Scalable, Decentralized Placement of Replicated Data** | Directly maps to Momo's `CRUSH-lite`. Read this to understand why clients and nodes can compute placement without a central metadata service. |
| **RADOS: A Scalable, Reliable Storage Service for Petabyte-scale Storage Clusters** | Useful for understanding the object store under Ceph and how placement, replication, recovery, and cluster maps fit together. |
| **Using Name-Based Mappings to Increase Hit Rates** - David G. Thaler, Chinya V. Ravishankar, 1998 | Foundational rendezvous/highest-random-weight hashing paper. Momo's CRUSH-lite uses weighted rendezvous-style scoring rather than full Ceph hierarchy traversal. |
| **Consistent Hashing and Random Trees: Distributed Caching Protocols for Relieving Hot Spots on the World Wide Web** - David Karger et al., 1997 | Gives the alternative placement model used by many distributed systems. Useful when comparing Momo's WRH/CRUSH-lite to ring-based placement. |
| **The Google File System** - Sanjay Ghemawat, Howard Gobioff, Shun-Tak Leung, 2003 | Contrasting architecture: centralized master metadata with chunkservers. Helps clarify why Momo avoids a central metadata server. PDF: <https://static.googleusercontent.com/media/research.google.com/en//archive/gfs-sosp2003.pdf> |
| **Dynamo: Amazon's Highly Available Key-value Store** | Important for consistent hashing, sloppy quorum, hinted handoff, vector clocks, anti-entropy, and operational availability trade-offs. |
| **File Systems Unfit as Distributed Storage Backends** - Abutalib Aghayev, Sage A. Weil, Michael Kuchnik, et al., 2019 | Relevant to Momo's pluggable `BlobStore` and raw backend direction. OpenAlex citations: 90. PDF: <https://dl.acm.org/doi/pdf/10.1145/3341301.3359656> |
| **The Case for Custom Storage Backends in Distributed Storage Systems** - Abutalib Aghayev, Sage A. Weil, Michael Kuchnik, et al., 2020 | Follow-up to the previous paper. Useful for evaluating when raw/block backends are worth the complexity. OpenAlex citations: 16. PDF: <https://dl.acm.org/doi/pdf/10.1145/3386362> |

## Replication and Consistency

| Paper | Why it matters for Momo |
|---|---|
| **Chain Replication for Supporting High Throughput and Availability** | Direct match for Momo's `ReplicationChain`. Explains head/tail roles, throughput, failure handling, and why chain writes trade latency for structured consistency. |
| **Dynamo: Amazon's Highly Available Key-value Store** | Best practical paper for quorum-style object storage, anti-entropy, eventual consistency, and operational degradation choices. |
| **Managing Update Conflicts in Bayou, a Weakly Connected Replicated Storage System** - Douglas Terry et al., 1995 | Useful for understanding conflict handling when replicas accept writes under weak connectivity. OpenAlex citations: 814. PDF: <https://dl.acm.org/doi/pdf/10.1145/224057.224070> |
| **Flexible Update Propagation for Weakly Consistent Replication** - Karin Petersen, Mike Spreitzer, Douglas Terry, et al., 1997 | Anti-entropy and propagation mechanics for weakly consistent replicas. OpenAlex citations: 477. PDF: <https://dl.acm.org/doi/pdf/10.1145/268998.266711> |
| **Time, Clocks, and the Ordering of Events in a Distributed System** - Leslie Lamport, 1978 | Foundational for Momo's timestamped replication-mode changes and for reasoning about event ordering. PDF: <https://lamport.azurewebsites.net/pubs/time-clocks.pdf> |
| **Timestamps in Message-Passing Systems That Preserve the Partial Ordering** - Colin Fidge, 1988 | Vector-clock foundation. Relevant to planned metadata versioning in `docs/momofs/ROADMAP.md`. |
| **Virtual Time and Global States of Distributed Systems** - Friedemann Mattern, 1988 | Another vector-clock/global-state foundation for future metadata conflict handling. |
| **Distributed Snapshots: Determining Global States of Distributed Systems** - K. Mani Chandy, Leslie Lamport, 1985 | Useful for future scrub, repair, and cluster-wide consistency checks. OpenAlex citations: 2443. PDF: <https://dl.acm.org/doi/pdf/10.1145/214451.214456> |
| **The Part-Time Parliament** - Leslie Lamport, 1998 | Paxos foundation. Momo does not implement Paxos, but this is useful background when comparing leases/quorum coordination with full consensus. OpenAlex citations: 2728. PDF: <https://dl.acm.org/doi/pdf/10.1145/279227.279229> |

## Gossip, Membership, and Coordination

| Paper | Why it matters for Momo |
|---|---|
| **SWIM: Scalable Weakly-consistent Infection-style Process Group Membership Protocol** | Direct match for `src/p2p/swim_test.go`, `src/p2p/gossip.go`, indirect pings, suspicion, and failure detection. |
| **Epidemic Algorithms for Replicated Database Maintenance** - Alan Demers et al., 1987 | Original anti-entropy/gossip reference. Useful for tombstone propagation and eventual convergence. DOI: <https://doi.org/10.1145/41840.41841> |
| **Bimodal Multicast** - Ken Birman, Mark Hayden, Ozalp Ozkasap, et al., 1999 | Practical probabilistic multicast trade-offs. OpenAlex citations: 652. PDF: <https://dl.acm.org/doi/pdf/10.1145/312203.312207> |
| **Gossip-based Aggregation in Large Dynamic Networks** - Mark Jelasity, Alberto Montresor, Ozalp Babaoglu, 2005 | Relevant if Momo expands metrics exchange, aggregate cluster health, or load balancing over gossip. OpenAlex citations: 717. PDF: <https://dl.acm.org/doi/pdf/10.1145/1082469.1082470> |
| **Lightweight Probabilistic Broadcast** - Patrick Eugster, Rachid Guerraoui, Sidath Handurukande, et al., 2003 | Helps reason about fanout, latency, and reliability trade-offs in gossip dissemination. OpenAlex citations: 400. PDF: <https://infoscience.epfl.ch/bitstreams/1a4e4eae-6fc4-4c5c-9b6d-7ad434bf4a1e/download> |
| **Census: Location-aware Membership Management for Large-scale Distributed Systems** - James Cowling, Dan R. K. Ports, Barbara Liskov, et al., 2009 | Relevant to Momo's region-aware WAN topology rule and future region-scoped gossip. OpenAlex citations: 36. PDF: <http://hdl.handle.net/1721.1/61401> |
| **Leases: An Efficient Fault-Tolerant Mechanism for Distributed File Cache Consistency** | Directly relevant to `src/p2p/lease.go`. Read before expanding lease-based delete or metadata coordination. |

## Transport and Protocol Design

| Paper | Why it matters for Momo |
|---|---|
| **The QUIC Transport Protocol: Design and Internet-Scale Deployment** | Best match for Momo's QUIC transport rationale: stream multiplexing, encryption, reduced handshake latency, and avoiding TCP head-of-line blocking at the transport/application stack boundary. |
| **Multipath QUIC** - Quentin De Coninck, Olivier Bonaventure, 2017 | Relevant to future WAN and multi-region transfer work. OpenAlex citations: 298. PDF: <https://orbi.umons.ac.be/bitstream/20.500.12907/49317/1/conext17-deconinck.pdf> |
| **Architectural Styles and the Design of Network-based Software Architectures** - Roy Fielding, 2000 | REST dissertation. Useful background for Momo's S3-compatible HTTP layer and why S3-style APIs compose well with object storage. URL: <https://www.ics.uci.edu/~fielding/pubs/dissertation/top.htm> |
| **Implementing Linearizability at Large Scale and Low Latency** - Collin Lee, Seo Jin Park, Ankita Kejriwal, et al., 2015 | Useful if Momo evolves stronger RPC semantics or exactly-once request handling. OpenAlex citations: 62. PDF: <http://dl.acm.org/ft_gateway.cfm?id=2815416&type=pdf> |

## Content-Addressable Storage and Deduplication

| Paper | Why it matters for Momo |
|---|---|
| **Venti: A New Approach to Archival Storage** | Best match for Momo's content-addressed blob storage and deduplication model. |
| **A Low-Bandwidth Network File System** - Athicha Muthitacharoen, Benjie Chen, David Mazieres, 2001 | Introduces content-defined chunking for network-efficient synchronization. Momo hashes whole objects today, but this is useful if chunked deduplication is added. PDF: <https://pdos.csail.mit.edu/papers/lbfs:sosp01/lbfs.pdf> |
| **Network Applications of Bloom Filters: A Survey** - Andrei Broder, Michael Mitzenmacher, 2004 | Relevant to planned Bloom filters for fast `Has()` across a cluster. OpenAlex citations: 2001. PDF: <https://www.internetmathematicsjournal.com/article/1393.pdf> |
| **From Hyper-dimensional Structures to Linear Structures: Maintaining Deduplicated Data's Locality** - Xiangyu Zou, Jingsong Yuan, Philip Shilane, et al., 2022 | Useful if Momo optimizes deduplicated storage locality and restore/read performance. OpenAlex citations: 106. PDF: <https://dl.acm.org/doi/pdf/10.1145/3507921> |
| **I-sieve: An Inline High Performance Deduplication System Used in Cloud Storage** - Jibin Wang, Zhigang Zhao, Zhaogang Xu, et al., 2015 | Useful for inline dedup throughput trade-offs. OpenAlex citations: 33. PDF: <https://ieeexplore.ieee.org/ielx7/5971803/7040506/07040510.pdf> |

## Secure E2EE and Confidential Deduplication

| Paper or standard | Why it matters for Momo |
|---|---|
| **DupLESS: Server-Aided Encryption for Deduplicated Storage** - Mihir Bellare, Sriram Keelveedhi, Thomas Ristenpart, 2013. PDF: <https://www.usenix.org/system/files/conference/usenixsecurity13/sec13-paper_bellare.pdf> | Directly relevant to Momo's secure deduplication direction: deduplication with server-aided key derivation instead of plain convergent encryption. |
| **Message-Locked Encryption and Secure Deduplication** - Mihir Bellare, Sriram Keelveedhi, Thomas Ristenpart, 2013. URL: <https://eprint.iacr.org/2012/631> | Formalizes message-locked encryption and its leakage trade-offs. Useful background for why Momo's design avoids unauthenticated convergent encryption. |
| **Efficient and Strongly-Secure OPRF from the CDH Assumption** - Stanislaw Jarecki, Aggelos Kiayias, Hugo Krawczyk, 2017. URL: <https://eprint.iacr.org/2017/111> | Foundation for OPRF-style blind key derivation used by confidential deduplication designs. |
| **RFC 9497: Oblivious Pseudorandom Functions (OPRFs) Using Prime-Order Groups** - Alex Davidson, Nick Sullivan, 2023. URL: <https://www.rfc-editor.org/rfc/rfc9497.html> | Current interoperable VOPRF/OPRF standard. Relevant to Momo's threshold OPRF API and future wire compatibility. |
| **How to Share a Secret** - Adi Shamir, 1979. PDF: <https://web.mit.edu/6.857/OldStuff/Fall03/ref/Shamir-HowToShareASecret.pdf> | Foundational threshold secret sharing. Directly maps to Momo's OPRF share reconstruction and quorum-oriented key derivation. |
| **RFC 5869: HMAC-based Extract-and-Expand Key Derivation Function (HKDF)** - Hugo Krawczyk, Pasi Eronen, 2010. URL: <https://www.rfc-editor.org/rfc/rfc5869.html> | Matches Momo's HKDF domain separation for token, content, at-rest, and OPRF-derived keys. |
| **NIST SP 800-38D: Recommendation for Block Cipher Modes of Operation: Galois/Counter Mode (GCM) and GMAC** - Morris Dworkin, 2007. PDF: <https://nvlpubs.nist.gov/nistpubs/Legacy/SP/nistspecialpublication800-38d.pdf> | Baseline reference for AES-GCM authentication and nonce uniqueness. Relevant to Momo's streaming AEAD nonce construction and non-reuse requirements. |

## Adaptive and Self-Managing Systems

| Paper | Why it matters for Momo |
|---|---|
| **Software Engineering for Self-Adaptive Systems: A Second Research Roadmap** | Best survey for Momo's polymorphic system and MomoFS adaptive-system design docs. Covers feedback-loop design, requirements, uncertainty, and runtime models. |
| **Software Engineering for Self-Adaptive Systems: A Research Roadmap** - Betty H. C. Cheng, Rogério de Lemos, Holger Giese, et al., 2009 | Earlier roadmap. OpenAlex citations: 95. URL: <https://lirias.kuleuven.be/handle/123456789/278886> |
| **Software Engineering Meets Control Theory** - Antonio Filieri, Martina Maggio, Konstantinos Angelopoulos, et al., 2015 | Useful if Momo's threshold-based metrics controller evolves into a more formal control system. OpenAlex citations: 69. PDF: <https://inria.hal.science/hal-01119461v1/file/paper.pdf> |
| **Self-adaptive Software Needs Quantitative Verification at Runtime** - Radu Calinescu, Carlo Ghezzi, Marta Kwiatkowska, et al., 2012 | Relevant to verifying that replication-mode changes preserve safety and service-level goals. OpenAlex citations: 253. PDF: <https://eprints.whiterose.ac.uk/id/eprint/75703/1/p69_calinescu.pdf> |
| **Applying Machine Learning in Self-adaptive Systems** - Omid Gheibi, Danny Weyns, Federico Quin, 2020 | Useful for later intelligent tiering or anomaly-response work. OpenAlex citations: 147. PDF: <https://dl.acm.org/doi/pdf/10.1145/3469440> |

## Future MomoFS Extensions

This section maps the planned MomoFS work in `docs/momofs/*.md` to research areas and papers. It is intentionally separate from the current-code sections above.

| MomoFS docs | Future design area | Papers to read |
|---|---|---|
| `CURRENT_ARCHITECTURE.md`, `LIMITATIONS.md` | Move from local-only metadata to distributed, replicated metadata; remove global namespace and listing gaps | **Ceph**, **RADOS**, **Dynamo**, **Consistent Hashing and Random Trees**, **Leases** |
| `DESIGN_PRINCIPLES.md`, `ARCHITECTURE.md`, `IMPLEMENTATION.md` | Read from any node, zero single point of failure, transparent proxying, metadata sharding, any-node gateway behavior | **Ceph**, **RADOS**, **Dynamo**, **The Google File System** as a contrasting centralized-master design |
| `SCRUB_HEALING.md` | Shallow scrub, deep scrub, bitrot detection, under-replication repair, split-brain metadata handling | **Dynamo** for Merkle anti-entropy, **Epidemic Algorithms**, **Distributed Snapshots**, **Store, Forget, and Check: Using Algebraic Signatures to Check Remotely Administered Storage** |
| `RECOVERY.md` | WAL-based recovery, incremental sync, vector clocks, Merkle divergence detection, parallel rebuild, directory operations | **Time, Clocks, and the Ordering of Events**, **Fidge vector clocks**, **Mattern virtual time**, **Dynamo**, **Leases** |
| `RECOVERY.md`, `ROADMAP.md` | Reed-Solomon erasure coding and repair-efficient recovery | **A Hitchhiker's Guide to Fast and Efficient Data Reconstruction in Erasure-coded Data Centers**, **Erasure Coding for Distributed Storage: An Overview**, **f4: Facebook's Warm BLOB Storage System** |
| `MULTI_TENANCY.md` | Tenant namespaces, quotas, per-tenant encryption, tenant audit logs, policy isolation | **Plutus: Scalable Secure File Sharing on Untrusted Storage**, **SiRiUS: Securing Remote Untrusted Storage**, **SUNDR: Secure Untrusted Data Repository** |
| `GDPR.md` | Right to erasure, crypto-shredding, portability, data residency, envelope encryption, key rotation | **Venti** for immutable CAS trade-offs, **Plutus**, **SiRiUS**, **POTSHARDS: Secure Long-Term Storage Without Encryption**, **Vanish: Increasing Data Privacy with Self-Destructing Data** |
| `AI_SEARCH.md` | Vector embeddings, HNSW semantic search, hybrid metadata/vector search, intelligent tiering | **Efficient and Robust Approximate Nearest Neighbor Search Using Hierarchical Navigable Small World Graphs**, **Milvus**, **Dense Text Retrieval Based on Pretrained Language Models: A Survey** |
| `PERFORMANCE_SECURITY.md` | Zero-allocation metadata hot path, Bloom filters, batched metadata writes, zero-copy streaming, bounded RPCs | **Network Applications of Bloom Filters**, **File Systems Unfit as Distributed Storage Backends**, **The Case for Custom Storage Backends in Distributed Storage Systems**, **The QUIC Transport Protocol** |
| `ADAPTIVE_SYSTEMS.md` | Stigmergy, self-healing, adaptive routing, quorum sensing, local-rule cluster behavior | **Software Engineering for Self-Adaptive Systems**, **Gossip-based Aggregation in Large Dynamic Networks**, **Software Engineering Meets Control Theory**, **Applying Machine Learning in Self-adaptive Systems** |
| `DESIGN_PRINCIPLES.md`, `COMPARISON.md` | HPC/cloud readiness, parallel reads, object gateway behavior, region-aware deployments | **Ceph**, **DAOS: A Scale-Out High Performance Storage Stack for Storage Class Memory**, **Multipath QUIC**, **Census** |

| Future feature | Papers to read |
|---|---|
| Distributed metadata with causal conflict handling | **Dynamo**, **Time, Clocks, and the Ordering of Events**, **Fidge vector clocks**, **Mattern virtual time** |
| Replica divergence detection and incremental repair | **Dynamo** for Merkle anti-entropy, **Epidemic Algorithms**, **Chandy-Lamport Distributed Snapshots** |
| Erasure coding | **A Hitchhiker's Guide to Fast and Efficient Data Reconstruction in Erasure-coded Data Centers** - K. V. Rashmi et al., 2014. OpenAlex citations: 212. PDF: <https://dl.acm.org/doi/pdf/10.1145/2619239.2626325> |
| Erasure coding overview | **Erasure Coding for Distributed Storage: An Overview** - S. B. Balaji, M. Nikhil Krishnan, Myna Vajha, et al., 2018. OpenAlex citations: 170 |
| Warm object storage with erasure coding | **f4: Facebook's Warm BLOB Storage System** - Subramanian Muralidhar et al., 2014. PDF: <https://www.usenix.org/system/files/conference/osdi14/osdi14-paper-muralidhar.pdf> |
| Semantic search | **Efficient and Robust Approximate Nearest Neighbor Search Using Hierarchical Navigable Small World Graphs** - Yu. A. Malkov, D. A. Yashunin, 2018. OpenAlex citations: 1700. arXiv: <https://arxiv.org/abs/1603.09320> |
| Vector database architecture | **Milvus: A Purpose-Built Vector Data Management System** - Jianguo Wang et al., 2021. OpenAlex citations: 337. PDF: <https://dl.acm.org/doi/pdf/10.1145/3448016.3457550> |
| Fast membership checks | **Network Applications of Bloom Filters: A Survey** |
| Region-aware membership | **Census: Location-aware Membership Management for Large-scale Distributed Systems** |

Additional papers for security, tenancy, and compliance-oriented MomoFS work:

| Paper | Why it matters for future MomoFS |
|---|---|
| **Plutus: Scalable Secure File Sharing on Untrusted Storage** - Mahesh Kallahalla, Erik Riedel, Ram Swaminathan, Qian Wang, Kevin Fu, 2003. PDF: <https://www.usenix.org/legacy/events/fast03/tech/full_papers/kallahalla/kallahalla.pdf> | Useful for per-tenant cryptographic isolation, key rotation, and sharing over untrusted storage. |
| **SiRiUS: Securing Remote Untrusted Storage** - Eu-Jin Goh, Hovav Shacham, Nagendra Modadugu, Dan Boneh, 2003. PDF: <https://crypto.stanford.edu/~dabo/pubs/papers/sirius.pdf> | Useful for encrypted object metadata, file integrity, and secure remote storage semantics. |
| **SUNDR: Secure Untrusted Data Repository** - Jinyuan Li, Maxwell Krohn, David Mazieres, Dennis Shasha, 2004. PDF: <https://www.usenix.org/legacy/events/osdi04/tech/full_papers/li_j/li_j.pdf> | Useful when thinking about malicious or inconsistent storage servers and fork consistency. |
| **POTSHARDS: Secure Long-Term Storage Without Encryption** - Mark W. Storer, Kevin M. Greenan, Ethan L. Miller, Kaladhar Voruganti, 2007. OpenAlex citations: 67. PDF: <https://escholarship.org/uc/item/3gz1t07k> | Relevant to long-term retention, secret splitting, and compliance-driven archival storage. |
| **Vanish: Increasing Data Privacy with Self-Destructing Data** - Roxana Geambasu, Tadayoshi Kohno, Amit A. Levy, Henry M. Levy, 2009. PDF: <https://www.usenix.org/legacy/event/sec09/tech/full_papers/geambasu.pdf> | Relevant background for crypto-shredding and time-bounded recoverability. |
| **Store, Forget, and Check: Using Algebraic Signatures to Check Remotely Administered Storage** - Thomas S. J. Schwarz, Ethan L. Miller, 2006. OpenAlex citations: 300. PDF: <https://escholarship.org/content/qt3gz9z0ws/qt3gz9z0ws.pdf> | Useful for deep scrub, remote possession checks, and detecting storage corruption without full local copies. |

## Suggested Reading Paths

For placement and storage internals: **Ceph**, **CRUSH**, **RADOS**, **Rendezvous hashing**, **Venti**, **File Systems Unfit as Distributed Storage Backends**.

For replication behavior: **Chain Replication**, **Dynamo**, **Bayou**, **Lamport clocks**, **Leases**.

For P2P and cluster health: **SWIM**, **Epidemic Algorithms**, **Gossip-based Aggregation**, **Census**.

For transport work: **QUIC Transport Protocol**, **Multipath QUIC**, **Fielding REST dissertation**.

For secure E2EE and confidential deduplication: **DupLESS**, **Message-Locked Encryption**, **OPRF RFC 9497**, **Shamir secret sharing**, **HKDF RFC 5869**, **NIST SP 800-38D**.

For adaptive replication and MomoFS self-healing: **Self-Adaptive Systems Roadmap**, **Software Engineering Meets Control Theory**, **Self-adaptive Software Needs Quantitative Verification at Runtime**.

For roadmap features: **HNSW**, **Milvus**, **Hitchhiker's Guide to Erasure Coding**, **f4**, **Bloom filters**, **Dynamo's Merkle/vector-clock sections**.
