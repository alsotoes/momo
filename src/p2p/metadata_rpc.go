package p2p

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

const (
	// MetadataRPCTTL is the TTL for cached metadata responses.
	MetadataRPCTTL = 60 * time.Second
	// MetadataRPCTimeout is the default per-call timeout for metadata RPCs.
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
	// RequestID correlates the reply with the waiting quorum call.
	RequestID uint64
	ShardKey  string
	Meta      map[string]interface{} // ObjectMeta fields as JSON
}

// ReplicateMetadataReply is the payload for a ReplicateMetadata RPC response.
type ReplicateMetadataReply struct {
	RequestID uint64
	Success   bool
	Error     string
}

// ReplicateVersionArgs is the payload for a ReplicateVersion RPC request
// (R10 follow-up, #1233): one version record plus the bucket's versioning
// status, so a replica can answer version queries without extra round trips.
type ReplicateVersionArgs struct {
	RequestID        uint64
	Bucket           string
	Key              string
	Record           storage.VersionRecord
	VersioningStatus string
}

// ReplicateVersionReply is the payload for a ReplicateVersion RPC response.
type ReplicateVersionReply struct {
	RequestID uint64
	Success   bool
	Error     string
}

// ListShardArgs is the payload for a ListShard RPC request.
type ListShardArgs struct {
	ShardKey          string
	Prefix            string
	Delimiter         string
	MaxKeys           int
	ContinuationToken string
	FetchOwner        bool
}

// ListShardReply is the payload for a ListShard RPC response.
type ListShardReply struct {
	Files                 []common.FileMetadata
	CommonPrefixes        []string
	NextContinuationToken string
	Error                 string
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

func EncodeListShardArgs(args *ListShardArgs) ([]byte, error) {
	return json.Marshal(args)
}

func DecodeListShardArgs(data []byte) (*ListShardArgs, error) {
	var args ListShardArgs
	err := json.Unmarshal(data, &args)
	return &args, err
}

func EncodeListShardReply(reply *ListShardReply) ([]byte, error) {
	return json.Marshal(reply)
}

func DecodeListShardReply(data []byte) (*ListShardReply, error) {
	var reply ListShardReply
	err := json.Unmarshal(data, &reply)
	return &reply, err
}

// Encode/Decode for the version replication payloads.

func EncodeReplicateVersionArgs(args *ReplicateVersionArgs) ([]byte, error) {
	return json.Marshal(args)
}

func DecodeReplicateVersionArgs(data []byte) (*ReplicateVersionArgs, error) {
	var args ReplicateVersionArgs
	err := json.Unmarshal(data, &args)
	return &args, err
}

func EncodeReplicateVersionReply(reply *ReplicateVersionReply) ([]byte, error) {
	return json.Marshal(reply)
}

func DecodeReplicateVersionReply(data []byte) (*ReplicateVersionReply, error) {
	var reply ReplicateVersionReply
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

	// versionPending correlates ReplicateVersion replies by request ID.
	versionPendingMu sync.Mutex
	versionPending   map[uint64]chan *ReplicateVersionReply

	// replicaPending correlates ReplicateMetadata replies by request ID so a
	// quorum write cannot be woken by an unrelated peer's response.
	replicaPendingMu sync.Mutex
	replicaPending   map[uint64]chan *ReplicateMetadataReply

	// RPCTimeout bounds a request/response call; zero means MetadataRPCTimeout.
	// Per-instance so tests can shorten it without racing a shared global.
	RPCTimeout time.Duration
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
		localID:        localID,
		transport:      transport,
		ring:           ring,
		store:          store,
		pending:        make(map[uint64]*pendingMetadata),
		versionPending: make(map[uint64]chan *ReplicateVersionReply),
		replicaPending: make(map[uint64]chan *ReplicateMetadataReply),
		RPCTimeout:     MetadataRPCTimeout,
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
	case MsgListShard:
		m.handleListShard(rpc)
	case MsgListShardResponse:
		m.handleListShardResponse(rpc)
	case MsgReplicateVersion:
		m.handleReplicateVersion(rpc)
	case MsgReplicateVersionResponse:
		m.handleReplicateVersionResponse(rpc)
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

	if name == "" || hash == "" {
		// Fail closed: a replica must never store a nameless record, which would
		// be unreachable and invisible to listing.
		reply := &ReplicateMetadataReply{RequestID: args.RequestID, Success: false, Error: "replica metadata missing name or hash"}
		payload, _ := EncodeReplicateMetadataReply(reply)
		_ = m.transport.Send(rpc.From, &RPC{Type: MsgReplicateMetadataResponse, Payload: payload})
		log.Printf("MetadataRPC: rejected metadata replica from peer %d: missing name or hash (errno=%d)", rpc.From, syscall.EBADMSG)
		return
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
	reply := &ReplicateMetadataReply{RequestID: args.RequestID, Success: err == nil}
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
	m.replicaPendingMu.Lock()
	ch, ok := m.replicaPending[reply.RequestID]
	m.replicaPendingMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- reply:
	default:
	}
}

// handleListShard processes an incoming ListShard request.
func (m *MetadataRPCProvider) handleListShard(rpc *RPC) {
	args, err := DecodeListShardArgs(rpc.Payload)
	if err != nil {
		log.Printf("MetadataRPC: failed to decode ListShard from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}

	// List files in the local store with the given prefix
	files, err := m.store.List()
	if err != nil {
		reply := &ListShardReply{Error: err.Error()}
		payload, _ := EncodeListShardReply(reply)
		m.transport.Send(rpc.From, &RPC{
			Type:    MsgListShardResponse,
			Payload: payload,
		})
		return
	}

	// Filter by shard key and prefix
	var filtered []common.FileMetadata
	for _, f := range files {
		if !strings.HasPrefix(f.Name, args.Prefix) {
			continue
		}
		// Check if this file belongs to the requested shard
		if args.ShardKey != "" {
			fileShardKey := ShardKey(f.Name)
			if fileShardKey != args.ShardKey {
				continue
			}
		}
		filtered = append(filtered, f)
	}

	// Simple pagination - just return up to MaxKeys
	if args.MaxKeys > 0 && len(filtered) > args.MaxKeys {
		filtered = filtered[:args.MaxKeys]
	}

	reply := &ListShardReply{
		Files: filtered,
	}
	payload, _ := EncodeListShardReply(reply)
	m.transport.Send(rpc.From, &RPC{
		Type:    MsgListShardResponse,
		Payload: payload,
	})
}

// handleListShardResponse processes a response to our ListShard request.
func (m *MetadataRPCProvider) handleListShardResponse(rpc *RPC) {
	reply, err := DecodeListShardReply(rpc.Payload)
	if err != nil {
		log.Printf("MetadataRPC: failed to decode ListShardResponse from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}
	m.completePending(rpc.From, reply, err)
}

// rpcTimeout returns the per-call timeout.
func (m *MetadataRPCProvider) rpcTimeout() time.Duration {
	if m.RPCTimeout > 0 {
		return m.RPCTimeout
	}
	return MetadataRPCTimeout
}

// handleReplicateVersion applies a replicated version record locally and
// acknowledges. Apply is idempotent; a stale record never demotes a newer one.
func (m *MetadataRPCProvider) handleReplicateVersion(rpc *RPC) {
	args, err := DecodeReplicateVersionArgs(rpc.Payload)
	if err != nil {
		log.Printf("MetadataRPC: failed to decode ReplicateVersion from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}
	reply := &ReplicateVersionReply{RequestID: args.RequestID, Success: true}
	if args.VersioningStatus != "" {
		if serr := m.store.SetBucketVersioning(args.Bucket, args.VersioningStatus); serr != nil {
			reply.Success = false
			reply.Error = serr.Error()
		}
	}
	if reply.Success {
		if aerr := m.store.PutVersionRecord(args.Bucket, args.Key, args.Record); aerr != nil {
			reply.Success = false
			reply.Error = aerr.Error()
		}
	}
	payload, _ := EncodeReplicateVersionReply(reply)
	_ = m.transport.Send(rpc.From, &RPC{
		Type:    MsgReplicateVersionResponse,
		Payload: payload,
	})
}

// handleReplicateVersionResponse wakes the caller waiting on this request.
func (m *MetadataRPCProvider) handleReplicateVersionResponse(rpc *RPC) {
	reply, err := DecodeReplicateVersionReply(rpc.Payload)
	if err != nil {
		log.Printf("MetadataRPC: failed to decode ReplicateVersionResponse from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}
	m.versionPendingMu.Lock()
	ch, ok := m.versionPending[reply.RequestID]
	m.versionPendingMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- reply:
	default:
	}
}

// ReplicateVersion sends one version record to a peer and waits for its
// acknowledgement, bounded by MetadataRPCTimeout.
func (m *MetadataRPCProvider) ReplicateVersion(peer int32, args *ReplicateVersionArgs) (*ReplicateVersionReply, error) {
	requestID := m.nextRequestID.Add(1)
	args.RequestID = requestID
	ch := make(chan *ReplicateVersionReply, 1)
	m.versionPendingMu.Lock()
	m.versionPending[requestID] = ch
	m.versionPendingMu.Unlock()
	defer func() {
		m.versionPendingMu.Lock()
		delete(m.versionPending, requestID)
		m.versionPendingMu.Unlock()
	}()

	payload, err := EncodeReplicateVersionArgs(args)
	if err != nil {
		return nil, fmt.Errorf("encode version replica: %w", err)
	}
	if serr := m.transport.Send(peer, &RPC{Type: MsgReplicateVersion, Payload: payload}); serr != nil {
		return nil, fmt.Errorf("send version replica to peer %d: %w", peer, serr)
	}
	select {
	case reply := <-ch:
		if !reply.Success {
			return reply, fmt.Errorf("peer %d rejected version record: %s: %w", peer, reply.Error, syscall.EIO)
		}
		return reply, nil
	case <-time.After(m.rpcTimeout()):
		return nil, fmt.Errorf("timeout replicating version to peer %d: %w", peer, syscall.ETIMEDOUT)
	}
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

// ReplicationFactor returns the ring's configured metadata replication factor.
func (r *Ring) ReplicationFactor() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.replicaM < 1 {
		return 1
	}
	return r.replicaM
}

// writeQuorum returns the number of acknowledgements a write needs: a strict
// majority of the replica set, capped at its size.
func writeQuorum(replicas int) int {
	if replicas <= 1 {
		return 1
	}
	return replicas/2 + 1
}

// replicateToPeer sends one metadata replica and waits for its acknowledgement.
func (m *MetadataRPCProvider) replicateToPeer(peer int32, args *ReplicateMetadataArgs) (*ReplicateMetadataReply, error) {
	requestID := m.nextRequestID.Add(1)
	// Copy per call: the same args pointer is shared by concurrent replica
	// sends, so mutating it here would race with their encodes.
	call := *args
	call.RequestID = requestID
	ch := make(chan *ReplicateMetadataReply, 1)
	m.replicaPendingMu.Lock()
	m.replicaPending[requestID] = ch
	m.replicaPendingMu.Unlock()
	defer func() {
		m.replicaPendingMu.Lock()
		delete(m.replicaPending, requestID)
		m.replicaPendingMu.Unlock()
	}()

	payload, err := EncodeReplicateMetadataArgs(&call)
	if err != nil {
		return nil, fmt.Errorf("encode metadata replica: %w", err)
	}
	if serr := m.transport.Send(peer, &RPC{Type: MsgReplicateMetadata, Payload: payload}); serr != nil {
		return nil, fmt.Errorf("send metadata replica to peer %d: %w", peer, serr)
	}
	select {
	case reply := <-ch:
		if !reply.Success {
			return reply, fmt.Errorf("peer %d rejected metadata: %s: %w", peer, reply.Error, syscall.EIO)
		}
		return reply, nil
	case <-time.After(m.rpcTimeout()):
		return nil, fmt.Errorf("timeout replicating metadata to peer %d: %w", peer, syscall.ETIMEDOUT)
	}
}

// PutMetadata writes one object's metadata to its replica set and waits for a
// quorum (Phase 2, #1142). The caller's vector clock is advanced for this node
// before fan-out so replicas can order concurrent writes.
//
// Partial success is reported as an error: the caller decides whether to retry.
// Acks already received are not rolled back, because a later write carrying a
// newer vector clock supersedes them.
func (m *MetadataRPCProvider) PutMetadata(args *PutMetadataArgs) (*PutMetadataReply, error) {
	replicas, err := m.prepareWrite(args)
	if err != nil {
		return &PutMetadataReply{Success: false, Error: err.Error()}, err
	}
	need := writeQuorum(len(replicas))

	acks := 0
	if m.isLocalReplica(replicas) {
		if aerr := m.applyLocally(args); aerr != nil {
			log.Printf("AUDIT: local metadata apply failed for %q: %v", args.Name, aerr)
		} else {
			acks++
		}
	}
	remoteAcks, remoteErr := m.replicateAll(replicas, args)
	acks += remoteAcks

	if acks < need {
		if remoteErr != nil {
			log.Printf("AUDIT: metadata quorum short for %q: %v", args.Name, remoteErr)
		}
		return m.quorumFailureReply(args, acks, len(replicas), need), fmt.Errorf(
			"metadata quorum not reached (%d/%d, need %d): %w", acks, len(replicas), need, syscall.EIO)
	}
	return &PutMetadataReply{Success: true, ExistingVClock: args.VectorClock}, nil
}

// prepareWrite validates the arguments, resolves the shard key and replica set,
// and advances this node's vector-clock entry.
func (m *MetadataRPCProvider) prepareWrite(args *PutMetadataArgs) ([]int32, error) {
	if args == nil {
		return nil, fmt.Errorf("nil PutMetadataArgs: %w", syscall.EINVAL)
	}
	if args.ShardKey == "" {
		args.ShardKey = ShardKey(args.Name)
	}
	replicas := args.MetadataReplicas
	if len(replicas) == 0 {
		replicas = m.ring.Replicas(args.ShardKey, m.ring.ReplicationFactor())
		args.MetadataReplicas = replicas
	}
	if len(replicas) == 0 {
		return nil, fmt.Errorf("no replicas for shard %q: %w", args.ShardKey, syscall.ENOTSUP)
	}
	// The write is causally after everything this node has seen.
	if int(m.localID) >= len(args.VectorClock) {
		grown := make([]uint64, m.localID+1)
		copy(grown, args.VectorClock)
		args.VectorClock = grown
	}
	args.VectorClock[m.localID]++
	return replicas, nil
}

// isLocalReplica reports whether this node belongs to the replica set.
func (m *MetadataRPCProvider) isLocalReplica(replicas []int32) bool {
	for _, id := range replicas {
		if id == m.localID {
			return true
		}
	}
	return false
}

// replicateAll sends the record to every remote replica concurrently and
// returns the acknowledgements collected, plus the first failure.
func (m *MetadataRPCProvider) replicateAll(replicas []int32, args *PutMetadataArgs) (int, error) {
	remote := make([]int32, 0, len(replicas))
	for _, id := range replicas {
		if id != m.localID {
			remote = append(remote, id)
		}
	}
	replicaArgs := &ReplicateMetadataArgs{
		ShardKey: args.ShardKey,
		Meta: map[string]interface{}{
			"Name":             args.Name,
			"Hash":             args.Hash,
			"Size":             args.Size,
			"RemotePath":       args.RemotePath,
			"VectorClock":      args.VectorClock,
			"ShardKey":         args.ShardKey,
			"MetadataReplicas": args.MetadataReplicas,
		},
	}
	acks := 0
	var firstErr error
	results := make(chan error, len(remote))
	for _, peer := range remote {
		go func(p int32) {
			_, err := m.replicateToPeer(p, replicaArgs)
			results <- err
		}(peer)
	}
	for i := 0; i < len(remote); i++ {
		if err := <-results; err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		acks++
	}
	return acks, firstErr
}

// quorumFailureReply builds the failure reply for a write that did not reach W.
func (m *MetadataRPCProvider) quorumFailureReply(args *PutMetadataArgs, acks, replicas, need int) *PutMetadataReply {
	return &PutMetadataReply{
		Success:        false,
		Error:          fmt.Sprintf("metadata quorum not reached: %d/%d acks, need %d", acks, replicas, need),
		ExistingVClock: args.VectorClock,
	}
}

// applyLocally writes the metadata to this node's store (used when the local
// node is a member of the replica set).
func (m *MetadataRPCProvider) applyLocally(args *PutMetadataArgs) error {
	if err := m.store.PutWithMetadata(
		args.Name,
		args.Hash,
		args.Size,
		args.RemotePath,
		nil,
		args.VectorClock,
		args.ShardKey,
		args.MetadataReplicas,
	); err != nil {
		return fmt.Errorf("apply metadata locally: %w", err)
	}
	return nil
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

// ListShard calls ListShard on the shard owner or replicas.
// This is the Phase 4 shard-aware list path.
func (m *MetadataRPCProvider) ListShard(args *ListShardArgs) (*ListShardReply, error) {
	// Phase 4: implement shard-aware listing
	return &ListShardReply{Error: "not implemented"}, nil
}
