package common

const (
	// Network-related constants

	// TCPSocketBufferSize defines the standard buffer size for TCP sockets.
	TCPSocketBufferSize = 1024
	// TimestampLength is the expected length of a timestamp string.
	TimestampLength = 19
	// FileInfoLength is the allocated length for file information strings.
	FileInfoLength = 64

	// ReplicationType defines the different modes of data replication.
	ReplicationType int = iota
	// ReplicationNone indicates that no replication is used.
	ReplicationNone
	// ReplicationChain indicates the use of chain replication.
	ReplicationChain
	// ReplicationSplay indicates the use of splay replication.
	ReplicationSplay
	// ReplicationPrimarySplay indicates a primary-splay replication strategy.
	ReplicationPrimarySplay

	// DummyEpoch is a placeholder epoch value for initialization.
	DummyEpoch = 1557906926566451195
)
