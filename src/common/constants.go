package common

const (
	// Network-related constants

	// TCPSocketBufferSize defines the standard buffer size for TCP sockets.
	TCPSocketBufferSize = 1024
	// TimestampLength is the expected length of a timestamp string.
	TimestampLength = 19
	// AuthTokenLength is the expected length of the authentication token.
	AuthTokenLength = 64
	// FileInfoLength is the allocated length for file information strings.
	FileInfoLength = 64
	// MaxFileSize is the maximum allowed file size (1GB).
	MaxFileSize = 1024 * 1024 * 1024
)

const (
	// ReplicationType defines the different modes of data replication.
	ReplicationType int = iota
	// ReplicationChain indicates the use of chain replication.
	ReplicationChain
	// ReplicationSplay indicates the use of splay replication.
	ReplicationSplay
	// ReplicationPrimarySplay indicates a primary-splay replication strategy.
	ReplicationPrimarySplay
	// ReplicationNone indicates that no replication is used.
	ReplicationNone

	// DummyEpoch is a placeholder epoch value for initialization.
	DummyEpoch = 1557906926566451195
)
