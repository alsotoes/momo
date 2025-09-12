package common

// FileMetadata stores the metadata for a file.
type FileMetadata struct {
	// Name is the name of the file.
	Name string
	// MD5 is the MD5 hash of the file.
	MD5 string
	// Size is the size of the file in bytes.
	Size int64
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
	// ReplicationOrder is the order of replication modes to use.
	ReplicationOrder []int
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

// Configuration holds the overall configuration for the application.
type Configuration struct {
	// Daemons is a list of daemons in the system.
	Daemons []*Daemon
	// Global is the global configuration.
	Global ConfigurationGlobal
	// Metrics is the metrics configuration.
	Metrics ConfigurationMetrics
}
