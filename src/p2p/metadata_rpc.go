package p2p

import (
	"encoding/json"
	"log"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/storage"
)

const (
	// MetadataRPCTTL is the TTL for cached metadata responses.
	MetadataRPCTTL = 60 * time.Second
	// MetadataRPCTimeout is the timeout for metadata RPC calls.
	MetadataRPCTimeout = 5 * time.Second
)

// PutMetadataArgs is the payload for a PutMetadata RPC request.
type PutMetadataArgs struct {
	Name             string
	Hash             string
	Size             int64
	RemotePath       string
	S3Headers        map[string]string
	DataReplicas     []int32
	MetadataReplicas []int32
	ShardKey         string
	VectorClock      []uint64
	Checksum         uint32
}

// PutMetadataReply is the payload for a PutMetadata RPC response.
type PutMetadataReply struct {
	Success        bool
	ExistingVClock []uint64
	Error          string
}

// ResolveMetadataArgs is the payload for a ResolveMetadata RPC request.
type ResolveMetadataArgs struct {
	Name     string
	ShardKey string
}

// ResolveMetadataReply is the payload for a ResolveMetadata RPC response.
type ResolveMetadataReply struct {
	Hash             string
	Size             int64
	RemotePath       string
	S3Headers        map[string]string
	DataReplicas     []int32
	MetadataReplicas []int32
	DeletedAt        time.Time
	VectorClock      []uint64
	Checksum         uint32
	Error            string
	ModTime          int64
}

// ReplicateMetadataArgs is the payload for a ReplicateMetadata RPC request.
type ReplicateMetadataArgs struct {
	ShardKey string
	Meta     map[string]interface{} // ObjectMeta fields as JSON
}

// ReplicateMetadataReply is the payload for a ReplicateMetadata RPC response.
type ReplicateMetadataReply struct {
	Success bool
	Error   string
}

// Encode/Decode functions for the payloads.

func EncodePutMetadataArgs(args *PutMetadataArgs) ([]byte, error) {
	data, err := json.Marshal(args)
	return data, err
}

func DecodePutMetadataArgs(data []byte) (*PutMetadataArgs, error) {
	var args PutMetadataArgs
	err := json.Unmarshal(data, &args)
	return &args, err
}

func EncodePutMetadataReply(reply *PutMetadataReply) ([]byte, error) {
	return json.Marshal(reply)
}

func DecodePutMetadataReply(data []byte) (*PutMetadataReply, error) {
	var reply PutMetadataReply
	err := json.Unmarshal(data, &reply)
	return &reply, err
}

func EncodeResolveMetadataArgs(args *ResolveMetadataArgs) ([]byte, error) {
	return json.Marshal(args)
}

func DecodeResolveMetadataArgs(data []byte) (*ResolveMetadataArgs, error) {
	var args ResolveMetadataArgs
	err := json.Unmarshal(data, &args)
	return &args, err
}

func EncodeResolveMetadataReply(reply *ResolveMetadataReply) ([]byte, error) {
	return json.Marshal(reply)
}

func DecodeResolveMetadataReply(data []byte) (*ResolveMetadataReply, error) {
	var reply ResolveMetadataReply
	err := json.Unmarshal(data, &reply)
	return &reply, err
}

func EncodeReplicateMetadataArgs(args *ReplicateMetadataArgs) ([]byte, error) {
	return json.Marshal(args)
}

func DecodeReplicateMetadataArgs(data []byte) (*ReplicateMetadataArgs, error) {
	var args ReplicateMetadataArgs
	err := json.Unmarshal(data, &args)
	return &args, err
}

func EncodeReplicateMetadataReply(reply *ReplicateMetadataReply) ([]byte, error) {
	return json.Marshal(reply)
}

func DecodeReplicateMetadataReply(data []byte) (*ReplicateMetadataReply, error) {
	var reply ReplicateMetadataReply
	err := json.Unmarshal(data, &reply)
	return &reply, err
}

// MetadataRPCProvider handles distributed metadata RPCs.
type MetadataRPCProvider struct {
	localID   int32
	transport Transport
	ring      *Ring
	store     *storage.CASStore

	nextRequestID atomic.Uint64
	pendingMu     sync.Mutex
	pending       map[uint64]*pendingMetadata
}

type pendingMetadata struct {
	responses chan *MetadataResponse
	expected  int
	quorum    int
	timeout   time.Duration
}

type MetadataResponse struct {
	From  int32
	Reply interface{} // PutMetadataReply, ResolveMetadataReply, or ReplicateMetadataReply
	Error error
}

// NewMetadataRPCProvider creates a new MetadataRPCProvider.
func NewMetadataRPCProvider(localID int32, transport Transport, ring *Ring, store *storage.CASStore) *MetadataRPCProvider {
	return &MetadataRPCProvider{
		localID:   localID,
		transport: transport,
		ring:      ring,
		store:     store,
		pending:   make(map[uint64]*pendingMetadata),
	}
}

// HandleRPC dispatches metadata RPCs. Called by the Gossiper's consumer loop.
func (m *MetadataRPCProvider) HandleRPC(rpc *RPC) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("MetadataRPCProvider HandleRPC panic recovered: %v (errno=%d)", r, syscall.EIO)
		}
	}()
	switch rpc.Type {
	case MsgPutMetadata:
		m.handlePutMetadata(rpc)
	case MsgPutMetadataResponse:
		m.handlePutMetadataResponse(rpc)
	case MsgResolveMetadata:
		m.handleResolveMetadata(rpc)
	case MsgResolveMetadataResponse:
		m.handleResolveMetadataResponse(rpc)
	case MsgReplicateMetadata:
		m.handleReplicateMetadata(rpc)
	case MsgReplicateMetadataResponse:
		m.handleReplicateMetadataResponse(rpc)
	}
}

// handlePutMetadata processes an incoming PutMetadata request.
func (m *MetadataRPCProvider) handlePutMetadata(rpc *RPC) {
	args, err := DecodePutMetadataArgs(rpc.Payload)
	if err != nil {
		log.Printf("MetadataRPC: failed to decode PutMetadata from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}

	// Write metadata with distributed fields
	err = m.store.PutWithMetadata(
		args.Name,
		args.Hash,
		args.Size,
		args.RemotePath,
		nil, // content is nil for metadata-only RPC; blob written separately
		args.VectorClock,
		args.ShardKey,
		args.MetadataReplicas,
	)
	reply := &PutMetadataReply{Success: err == nil}
	if err != nil {
		reply.Error = err.Error()
	}
	payload, _ := EncodePutMetadataReply(reply)
	m.transport.Send(rpc.From, &RPC{
		Type:    MsgPutMetadataResponse,
		Payload: payload,
	})
}

// handlePutMetadataResponse processes a response to our PutMetadata request.
func (m *MetadataRPCProvider) handlePutMetadataResponse(rpc *RPC) {
	reply, err := DecodePutMetadataReply(rpc.Payload)
	if err != nil {
		log.Printf("MetadataRPC: failed to decode PutMetadataResponse from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}
	m.completePending(rpc.From, reply, err)
}

// handleResolveMetadata processes an incoming ResolveMetadata request.
func (m *MetadataRPCProvider) handleResolveMetadata(rpc *RPC) {
	args, err := DecodeResolveMetadataArgs(rpc.Payload)
	if err != nil {
		log.Printf("MetadataRPC: failed to decode ResolveMetadata from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}

	// Read metadata from local store
	meta, err := m.store.GetMeta(args.Name)
	reply := &ResolveMetadataReply{}
	if err != nil {
		if err == syscall.ENOENT {
			reply.Error = "not found"
		} else {
			reply.Error = err.Error()
		}
	} else {
		reply.Hash = meta.Hash
		reply.Size = meta.Size
		reply.RemotePath = meta.RemotePath
		reply.S3Headers = meta.S3Headers
		// ModTime needs to be converted
		reply.ModTime = meta.ModTime
	}
	payload, _ := EncodeResolveMetadataReply(reply)
	m.transport.Send(rpc.From, &RPC{
		Type:    MsgResolveMetadataResponse,
		Payload: payload,
	})
}

// handleResolveMetadataResponse processes a response to our ResolveMetadata request.
func (m *MetadataRPCProvider) handleResolveMetadataResponse(rpc *RPC) {
	reply, err := DecodeResolveMetadataReply(rpc.Payload)
	if err != nil {
		log.Printf("MetadataRPC: failed to decode ResolveMetadataResponse from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}
	m.completePending(rpc.From, reply, err)
}

// handleReplicateMetadata processes an incoming ReplicateMetadata request.
func (m *MetadataRPCProvider) handleReplicateMetadata(rpc *RPC) {
	args, err := DecodeReplicateMetadataArgs(rpc.Payload)
	if err != nil {
		log.Printf("MetadataRPC: failed to decode ReplicateMetadata from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}

	// Decode the ObjectMeta from the args.Meta map
	// For now, we'll use the simpler approach of writing the metadata directly
	// The args.Meta should contain the fields from PutMetadataArgs
	var name, hash, shardKey, remotePath string
	var size int64
	var vectorClock []uint64
	var metadataReplicas []int32

	if args.Meta != nil {
		if v, ok := args.Meta["Name"].(string); ok {
			name = v
		}
		if v, ok := args.Meta["Hash"].(string); ok {
			hash = v
		}
		if v, ok := args.Meta["Size"].(float64); ok {
			size = int64(v)
		}
		if v, ok := args.Meta["RemotePath"].(string); ok {
			remotePath = v
		}
		if v, ok := args.Meta["ShardKey"].(string); ok {
			shardKey = v
		}
		if v, ok := args.Meta["VectorClock"].([]interface{}); ok {
			vectorClock = make([]uint64, len(v))
			for i, val := range v {
				if f, ok := val.(float64); ok {
					vectorClock[i] = uint64(f)
				}
			}
		}
		if v, ok := args.Meta["MetadataReplicas"].([]interface{}); ok {
			metadataReplicas = make([]int32, len(v))
			for i, val := range v {
				if f, ok := val.(float64); ok {
					metadataReplicas[i] = int32(f)
				}
			}
		}
	}

	err = m.store.PutWithMetadata(
		name,
		hash,
		size,
		remotePath,
		nil,
		vectorClock,
		shardKey,
		metadataReplicas,
	)
	reply := &ReplicateMetadataReply{Success: err == nil}
	if err != nil {
		reply.Error = err.Error()
	}
	payload, _ := EncodeReplicateMetadataReply(reply)
	m.transport.Send(rpc.From, &RPC{
		Type:    MsgReplicateMetadataResponse,
		Payload: payload,
	})
}

// handleReplicateMetadataResponse processes a response to our ReplicateMetadata request.
func (m *MetadataRPCProvider) handleReplicateMetadataResponse(rpc *RPC) {
	reply, err := DecodeReplicateMetadataReply(rpc.Payload)
	if err != nil {
		log.Printf("MetadataRPC: failed to decode ReplicateMetadataResponse from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}
	m.completePending(rpc.From, reply, err)
}

// completePending wakes up a waiting caller with the response.
func (m *MetadataRPCProvider) completePending(from int32, reply interface{}, err error) {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	// Find the pending request that expects a response from this peer
	for _, p := range m.pending {
		p.responses <- &MetadataResponse{From: from, Reply: reply, Error: err}
	}
}

// PutMetadata calls PutMetadata on the shard owner and waits for quorum.
// This is the Phase 2 quorum write path.
func (m *MetadataRPCProvider) PutMetadata(args *PutMetadataArgs) (*PutMetadataReply, error) {
	// Phase 1: just compile; Phase 2 will implement quorum logic
	return &PutMetadataReply{Success: true}, nil
}

// ResolveMetadata calls ResolveMetadata on the shard owner or a replica.
// This is the Phase 2/3 read path.
func (m *MetadataRPCProvider) ResolveMetadata(args *ResolveMetadataArgs) (*ResolveMetadataReply, error) {
	// Phase 1: just compile; Phase 2/3 will implement read logic
	return &ResolveMetadataReply{Error: "not implemented"}, nil
}

// ReplicateMetadata calls ReplicateMetadata on a replica.
// This is the Phase 2 replication path.
func (m *MetadataRPCProvider) ReplicateMetadata(args *ReplicateMetadataArgs) (*ReplicateMetadataReply, error) {
	// Phase 1: just compile; Phase 2 will implement replication
	return &ReplicateMetadataReply{Success: true}, nil
}
