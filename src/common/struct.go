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
	// PolymorphicSystem enables or disables the polymorphic system.
	PolymorphicSystem bool
	// CACertPath is the path to a PEM-encoded CA certificate file used to verify
	// QUIC peer certificates. When empty, InsecureSkipVerify is used with a warning.
	CACertPath string
	// TLSCertPath is the path to a PEM-encoded TLS certificate for TCP protocols.
	// When empty, TCP connections use plaintext (backward compatible).
	TLSCertPath string
	// TLSKeyPath is the path to a PEM-encoded TLS private key for TCP protocols.
	// When empty, TCP connections use plaintext (backward compatible).
	TLSKeyPath string
	// TLSInsecure skips TLS certificate verification. Defaults to false.
	// When true, QUIC InsecureSkipVerify is used and TCP TLS skip verification.
	// Must be explicitly set to true to opt in; not recommended for production.
	TLSInsecure bool
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
	// S3PathStyle uses path-style addressing (bucket in URL path) instead of virtual-host style.
	S3PathStyle bool
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
