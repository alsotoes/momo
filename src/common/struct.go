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
	// ReplicationFactor is the number of replicas to maintain for each object.
	ReplicationFactor int
	// PolymorphicSystem enables or disables the polymorphic system.
	PolymorphicSystem bool
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
	// ScatterGatherTimeout is the timeout for scatter-gather queries, in seconds.
	ScatterGatherTimeout int
	// LeaseTimeout is the default lease duration for consensus operations, in seconds.
	LeaseTimeout int
}

// ConfigurationStorage holds the storage and garbage collection configuration.
type ConfigurationStorage struct {
	// GCInterval is how often the garbage collector runs, in seconds.
	GCInterval int
	// TombstoneRetention is how long tombstones are kept, in seconds.
	TombstoneRetention int
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
