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
	// MaxFileSize is the maximum allowed size for a file transfer (1GB).
	MaxFileSize = 1024 * 1024 * 1024
	// MaxPathLength is the maximum allowed length for a virtual path string.
	MaxPathLength = 4096
)

const (
	// ReplicationNone indicates that no replication is used.
	ReplicationNone int = iota
	// ReplicationChain indicates the use of chain replication.
	ReplicationChain
	// ReplicationSplay indicates the use of splay replication.
	ReplicationSplay
	// ReplicationPrimarySplay indicates a primary-splay replication strategy.
	ReplicationPrimarySplay

	// ModeList indicates a request to list files over Momo protocol.
	ModeList = int('L')
	// ModeDelete indicates a request to delete a file over Momo protocol.
	ModeDelete = int('D')
	// ModeGet indicates a request to retrieve a file over Momo protocol.
	ModeGet = int('G')
	// ModeOPRFEval indicates a threshold-OPRF evaluation request: the client
	// sends a blinded dedup tag and receives share evaluations from the daemon
	// quorum to derive a content key without revealing the tag to any server.
	ModeOPRFEval = int('O')

	// DummyEpoch is a placeholder epoch value for initialization.
	DummyEpoch = 1557906926566451195
)
