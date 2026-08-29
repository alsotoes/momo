package common

// FileMetadata stores the metadata for a file.
type FileMetadata struct {
	// Name is the name of the file.
	Name string
	// Hash is the SHA-256 hash of the file.
	Hash string
	// Size is the size of the file in bytes.
	Size int64
	// RemotePath is the virtual folder or directory path of the file.
	RemotePath string
	// ModTime is the last modification time in Unix nanoseconds.
	// A value of 0 means the modification time is unknown.
	ModTime int64
	// S3Headers holds optional S3 object metadata (Content-Type, Cache-Control,
	// Content-Disposition, Content-Encoding, Expires, and x-amz-meta-* user
	// headers) captured from an S3 PUT and echoed on S3 GET/HEAD. It is additive
	// only: the replication-critical fields above (Name, Hash, Size, RemotePath,
	// ModTime) remain authoritative and are the only fields carried by the fixed
	// momo wire framing. S3 headers are stored at rest per object and propagated
	// between peers as an additive X-Momo-S3-Meta header (base64-encoded), so
	// peers without support simply store/echo no headers.
	S3Headers map[string]string
	// Checksums holds optional additive integrity checksums (issue #903).
	// Protocol-agnostic: any surface maps its client checksums onto these
	// ChecksumRefs, verified centrally by the shared ingest path (getFile) and,
	// opt-in, on retrieval. Like S3Headers, it is additive only: it is carried
	// between peers as an additive extension so peers without support are
	// unaffected, and it is NEVER the content-address (Hash remains that).
	Checksums []ChecksumRef
}

// ReplicationData stores the information about a replication mode change.
type ReplicationData struct {
	// Old is the old replication mode.
	Old int `json:"old"`
	// New is the new replication mode.
	New int `json:"new"`
	// TimeStamp is the timestamp of the replication mode change.
	TimeStamp int64 `json:"timestamp"`
}

// Daemon represents a daemon in the system.
type Daemon struct {
	// Host is the address of the daemon.
	Host string
	// ChangeReplication is the endpoint for changing the replication mode.
	ChangeReplication string
	// Data is the endpoint for data operations.
	Data string
	// Drive is the drive used by the daemon.
	Drive string
	// FailureDomain is the optional failure-domain label (rack/zone/DC) of the
	// daemon (R1, #929). Empty means unclassified: the node shares the default
	// failure domain with every other unclassified node.
	FailureDomain string
	// MetricsBindHost is an optional per-daemon bind address (host) for the
	// /metrics endpoint. Empty falls back to the global [metrics] bind host.
	MetricsBindHost string
	// MetricsBindPort is an optional per-daemon bind port for the /metrics
	// endpoint. 0 (unset) falls back to the global [metrics] prometheus_port.
	MetricsBindPort int
	// OPRFShare is the hex-encoded 256-bit Shamir share of the threshold OPRF
	// secret assigned to this daemon. Required when oprf_enabled is true.
	// Each daemon holds a distinct share; no daemon holds the full secret.
	OPRFShare string
	// OPRFShareIndex is the Shamir evaluation point (daemon index + 1) of
	// OPRFShare. Defaults to the daemon's position in the config when unset.
	OPRFShareIndex int
}

// ConfigurationGlobal holds the global configuration for the application.
type ConfigurationGlobal struct {
	// Debug enables or disables debug mode.
	Debug bool
	// Protocol defines the network stack to use (e.g., momo-tcp, momo-quic, s3-tcp).
	Protocol string
	// AuthToken is the authentication token used for node-to-node and client-to-node communication.
	AuthToken string
	// ReplicationOrder is the order of replication modes to use.
	ReplicationOrder []int
	// ClientSideReplicationModes lists mode IDs that require a momo-aware client.
	// External S3 clients (e.g., aws-cli) cannot use these modes; the server
	// downgrades to the next server-side mode in ReplicationOrder per-connection.
	ClientSideReplicationModes []int
	// ReplicationFactor is the number of replicas to maintain for each object.
	ReplicationFactor int
	// MinimumDurabilityFactor is the minimum achievable replica count the
	// metrics-driven controller may select (issue #822). A value of 0 (default)
	// disables the floor so the controller behaves as before. When > 0, the
	// controller refuses to automatically degrade to a mode whose achievable
	// replica count is below this floor, holding the current higher-durability
	// mode and logging the refusal instead of silently losing durability.
	MinimumDurabilityFactor int
	// WriteQuorum is the minimum number of durable replicas required before a
	// write is acknowledged (R3-C2, #931). Default 1; valid range
	// [1, ReplicationFactor]; validated at config load. A write that cannot
	// reach `write_quorum` durable replicas fails instead of silently acking.
	WriteQuorum int
	// PolymorphicSystem enables or disables the polymorphic system.
	PolymorphicSystem bool
	// CACertPath is the path to a PEM-encoded CA certificate file used to verify
	// QUIC/TCP peer certificates and to require client certificates (mutual TLS)
	// on servers. When empty, InsecureSkipVerify is used with a warning.
	CACertPath string
	// TLSCertPath is the path to a PEM-encoded TLS certificate, used for both
	// TCP and QUIC (the QUIC listener presents it instead of a self-signed cert).
	// When empty, TCP connections use plaintext (backward compatible) and QUIC
	// listeners fall back to a self-signed certificate.
	TLSCertPath string
	// TLSKeyPath is the path to a PEM-encoded TLS private key, matching TLSCertPath.
	// When empty, TCP connections use plaintext (backward compatible).
	TLSKeyPath string
	// TLSInsecure skips TLS certificate verification. Defaults to false.
	// When true, QUIC InsecureSkipVerify is used and TCP TLS skip verification.
	// Must be explicitly set to true to opt in; not recommended for production.
	TLSInsecure bool
	// EncryptionEnabled controls whether E2EE encryption is applied to content.
	// When true, all content is encrypted with AES-GCM-256 before storage/transfer.
	EncryptionEnabled bool
	// EncryptionKey is the 64-char hex-encoded 256-bit master encryption key.
	// Required when EncryptionEnabled is true.
	EncryptionKey string
	// EncryptionTenant is the tenant identifier for per-tenant key derivation.
	// Defaults to "default" when empty. Used with HKDF-SHA256 to derive per-tenant keys.
	EncryptionTenant string
	// OPRFEnabled enables threshold-OPRF confidential dedup. Defaults to
	// EncryptionEnabled when not set explicitly.
	OPRFEnabled bool
	// OPRFThreshold is the minimum number of OPRF share evaluations required
	// to derive a content key. Defaults to the number of configured daemons
	// when 0. Must satisfy 1 <= OPRFThreshold <= len(daemons).
	OPRFThreshold int
	// E2EEKey is the 64-hex 256-bit client-held master key for native protocol
	// (momo-tcp/momo-quic) envelope encryption. When set, content is wrapped in
	// a self-describing momo E2EE envelope (per-object data key under this
	// client-held master key) so the serving node never sees plaintext
	// (zero-trust vs the serving node, issue #780). The key is client-held only
	// and must never be the server's EncryptionKey.
	E2EEKey string
	// E2EEKeyID is the key identifier stored in the envelope header. Defaults
	// to "default" when empty.
	E2EEKeyID string
	// AuthBackoffDelay is the base delay (in milliseconds) applied to the
	// adaptive backoff after a failed authentication, per source. A value of 0
	// (default) disables auth throttling entirely so existing behavior is
	// unchanged. When > 0, consecutive failures from a source grow the delay
	// exponentially and, past a threshold, trigger a temporary lockout.
	AuthBackoffDelay int
}

// ConfigurationMetrics holds the metrics configuration for the application.
type ConfigurationMetrics struct {
	// Interval is the interval at which to collect metrics.
	Interval int
	// MaxThreshold is the maximum threshold for metrics.
	MaxThreshold float64
	// MinThreshold is the minimum threshold for metrics.
	MinThreshold float64
	// FallbackInterval is the interval at which to fall back to a lower replication mode.
	FallbackInterval int
	// PrometheusPort is the port for the Prometheus /metrics endpoint (0 = disabled).
	PrometheusPort int
	// PrometheusBindHost is the default bind address for the /metrics endpoint.
	// Empty binds all interfaces.
	PrometheusBindHost string
	// EnableLatencyHistograms opts-in the momo_request_latency_seconds and
	// momo_replication_latency_seconds histograms (R5 phase 4, #933). When
	// false (default) there is zero overhead on the request path.
	EnableLatencyHistograms bool
}

// ConfigurationP2P holds the P2P transport and gossip configuration.
type ConfigurationP2P struct {
	// Enabled controls whether the P2P transport starts alongside the main listener.
	Enabled bool
	// GossipPort is the port for P2P gossip communication.
	GossipPort string
	// GossipInterval is the heartbeat interval in seconds.
	GossipInterval int
	// SuspicionTimeout is the timeout before a peer is marked suspect, in seconds.
	SuspicionTimeout int
	// Fanout is the number of random peers to gossip to per heartbeat.
	Fanout int
	// PingTimeout is the timeout for a direct ping ack, in milliseconds.
	PingTimeout int
	// IndirectPingCount is the number of peers to ask for indirect pings.
	IndirectPingCount int
	// ScatterGatherTimeout is the timeout for scatter-gather queries, in seconds.
	ScatterGatherTimeout int
	// LeaseTimeout is the default lease duration for consensus operations, in seconds.
	LeaseTimeout int
	// TLSCertFile is the path to the TLS certificate file for P2P transport.
	TLSCertFile string
	// TLSKeyFile is the path to the TLS private key file for P2P transport.
	TLSKeyFile string
	// TLSCAFile is the path to the CA certificate file for verifying peer certificates.
	TLSCAFile string
}

// ConfigurationStorage holds the storage and garbage collection configuration.
type ConfigurationStorage struct {
	// Backend selects the storage backend type.
	// Valid values: "local" (default), "nfs", "s3", "raw".
	// An empty string defaults to "local".
	Backend string
	// GCInterval is how often the garbage collector runs, in seconds.
	GCInterval int
	// TombstoneRetention is how long tombstones are kept, in seconds.
	TombstoneRetention int
	// ScrubInterval is how often the background integrity scrub runs, in seconds.
	ScrubInterval int
	// VerifyOnRead re-derives the blob SHA-256 at read EOF and fails reads when
	// the content no longer matches its address key. Defaults to true (issue #924).
	VerifyOnRead bool
	// RebuildInterval is how often the background self-heal rebuild loop runs,
	// in seconds (R2, #930). 0 or negative falls back to the default (300).
	RebuildInterval int
	// DegradedRead enables survivor-set read fallback when a blob's local copy
	// is missing but a verified survivor replica holds it (R2-C1, #930).
	// Defaults to true.
	DegradedRead bool
	// RebuildWorkers bounds the number of concurrent blob repairs in a single
	// rebuild pass (R2-C6, #930). 0 or negative falls back to the default (4).
	RebuildWorkers int
	// Durability is the R3 write-durability profile (R3-C1, #931):
	// "fsync" (default), "group-commit", or "none". Empty defaults to fsync.
	// Invalid values are rejected at config load.
	Durability string
	// S3Endpoint is the S3-compatible API endpoint URL.
	S3Endpoint string
	// S3Region is the S3 region name.
	S3Region string
	// S3Bucket is the S3 bucket name for blob storage.
	S3Bucket string
	// S3AccessKey is the S3 access key ID for authentication.
	S3AccessKey string
	// S3SecretKey is the S3 secret access key for authentication.
	S3SecretKey string
	// S3ServerAccessKey is the SigV4 access key ID accepted by this node's S3
	// gateway for inbound external clients. When set, inbound SigV4 requests
	// must present this access key and be signed with S3ServerSecretKey,
	// decoupling gateway credentials from the momo auth token (issue #656).
	// When empty, the legacy single-token mode applies: access key and secret
	// are both derived from the global auth token.
	S3ServerAccessKey string
	// S3ServerSecretKey is the SigV4 secret key used to verify inbound SigV4
	// signatures against this node's S3 gateway. Only meaningful together with
	// S3ServerAccessKey (issue #656).
	S3ServerSecretKey string
	// S3PathStyle uses path-style addressing (bucket in URL path) instead of virtual-host style.
	S3PathStyle bool
	// S3Insecure allows an http:// S3 endpoint (cleartext). Defaults to false;
	// when false, an http:// endpoint is rejected to prevent silent credential
	// and payload disclosure. When true, a prominent warning is logged at
	// startup. https:// endpoints are always accepted regardless of this flag.
	S3Insecure bool
	// RawDevicePath is the path to the raw block device for blob storage.
	// Overrides Daemon.Drive if set.
	RawDevicePath string
}

// Configuration holds the overall configuration for the application.
type Configuration struct {
	// Daemons is a list of daemons in the system.
	Daemons []*Daemon
	// Global is the global configuration.
	Global ConfigurationGlobal
	// Metrics is the metrics configuration.
	Metrics ConfigurationMetrics
	// P2P is the P2P transport configuration.
	P2P ConfigurationP2P
	// Storage is the storage and GC configuration.
	Storage ConfigurationStorage
}
