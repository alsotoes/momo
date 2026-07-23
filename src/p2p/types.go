package p2p

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// PeerState represents the liveness state of a peer in the cluster.
type PeerState int32

const (
	PeerStateAlive    PeerState = 0
	PeerStateSuspect  PeerState = 1
	PeerStateOffline  PeerState = 2
)

// Peer represents a remote node in the P2P network.
type Peer struct {
	ID        int32
	Addr      string
	state     atomic.Int32
	lastSeen  atomic.Int64
	conn      net.Conn
	mu        sync.Mutex
}

// NewPeer creates a new Peer with the given ID and address.
func NewPeer(id int32, addr string) *Peer {
	p := &Peer{
		ID:   id,
		Addr: addr,
	}
	p.state.Store(int32(PeerStateAlive))
	p.lastSeen.Store(time.Now().UnixNano())
	return p
}

// State returns the current peer state.
func (p *Peer) State() PeerState {
	return PeerState(p.state.Load())
}

// SetState atomically updates the peer state.
func (p *Peer) SetState(s PeerState) {
	p.state.Store(int32(s))
}

// LastSeen returns the last heartbeat timestamp in UnixNano.
func (p *Peer) LastSeen() time.Time {
	return time.Unix(0, p.lastSeen.Load())
}

// Touch updates the last seen timestamp to now.
func (p *Peer) Touch() {
	p.lastSeen.Store(time.Now().UnixNano())
}

// SetConn sets the underlying network connection for this peer.
func (p *Peer) SetConn(c net.Conn) {
	p.mu.Lock()
	p.conn = c
	p.mu.Unlock()
}

// Conn returns the underlying network connection, if any.
func (p *Peer) Conn() net.Conn {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn
}

// MessageType identifies the kind of gossip message.
type MessageType uint8

const (
	MsgHeartbeat    MessageType = 1
	MsgMembership   MessageType = 2
	MsgSuspect      MessageType = 3
	MsgQuery        MessageType = 4
	MsgQueryResponse MessageType = 5
	MsgLeaseRequest  MessageType = 6
	MsgLeaseGrant    MessageType = 7
	MsgLeaseRelease  MessageType = 8
)

// RPC is a remote procedure call exchanged between peers.
// Wire format (binary, length-prefixed):
//
//	[4 bytes: total length] [1 byte: msg type] [4 bytes: from ID] [N bytes: payload]
type RPC struct {
	From    int32
	Type    MessageType
	Payload []byte
}

// Encode serializes an RPC into a binary frame.
// The returned slice is self-contained (no references to r.Payload).
func (r *RPC) Encode() []byte {
	totalLen := 1 + 4 + len(r.Payload)
	buf := make([]byte, 4+totalLen)
	binary.BigEndian.PutUint32(buf[0:4], uint32(totalLen))
	buf[4] = byte(r.Type)
	binary.BigEndian.PutUint32(buf[5:9], uint32(r.From))
	copy(buf[9:], r.Payload)
	return buf
}

// DecodeRPC reads a single RPC frame from the reader.
// It expects the 4-byte length prefix followed by the message body.
func DecodeRPC(r io.Reader) (*RPC, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("failed to read RPC length: %v: %w", err, syscall.EIO)
	}
	totalLen := binary.BigEndian.Uint32(lenBuf[:])
	if totalLen < 5 || totalLen > 1<<20 {
		return nil, fmt.Errorf("invalid RPC length %d: %w", totalLen, syscall.EBADMSG)
	}
	body := make([]byte, totalLen)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("failed to read RPC body: %v: %w", err, syscall.EIO)
	}
	return &RPC{
		Type:    MessageType(body[0]),
		From:    int32(binary.BigEndian.Uint32(body[1:5])),
		Payload: body[5:],
	}, nil
}

// HeartbeatPayload is the payload of a MsgHeartbeat RPC.
// It contains the sender's known peer list for membership dissemination.
// Wire format (binary):
//
//	[4 bytes: peer count] [for each peer: 4 bytes ID + 2 bytes addr len + addr bytes]
type HeartbeatPayload struct {
	Peers []PeerInfo
}

// PeerInfo is a compact peer representation for gossip payloads.
type PeerInfo struct {
	ID   int32
	Addr string
}

// Encode serializes a HeartbeatPayload into binary.
func (h *HeartbeatPayload) Encode() []byte {
	count := len(h.Peers)
	size := 4
	for _, p := range h.Peers {
		size += 4 + 2 + len(p.Addr)
	}
	buf := make([]byte, size)
	binary.BigEndian.PutUint32(buf[0:4], uint32(count))
	off := 4
	for _, p := range h.Peers {
		binary.BigEndian.PutUint32(buf[off:off+4], uint32(p.ID))
		off += 4
		addrLen := len(p.Addr)
		binary.BigEndian.PutUint16(buf[off:off+2], uint16(addrLen))
		off += 2
		copy(buf[off:], p.Addr)
		off += addrLen
	}
	return buf
}

// DecodeHeartbeatPayload deserializes a HeartbeatPayload from binary.
func DecodeHeartbeatPayload(data []byte) (*HeartbeatPayload, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("heartbeat payload too short: %w", syscall.EBADMSG)
	}
	count := int(binary.BigEndian.Uint32(data[0:4]))
	off := 4
	peers := make([]PeerInfo, 0, count)
	for i := 0; i < count; i++ {
		if off+6 > len(data) {
			return nil, fmt.Errorf("truncated peer entry %d: %w", i, syscall.EBADMSG)
		}
		id := int32(binary.BigEndian.Uint32(data[off : off+4]))
		off += 4
		addrLen := int(binary.BigEndian.Uint16(data[off : off+2]))
		off += 2
		if off+addrLen > len(data) {
			return nil, fmt.Errorf("truncated peer addr %d: %w", i, syscall.EBADMSG)
		}
		addr := string(data[off : off+addrLen])
		off += addrLen
		peers = append(peers, PeerInfo{ID: id, Addr: addr})
	}
	return &HeartbeatPayload{Peers: peers}, nil
}

// QueryType identifies the kind of scatter-gather query.
type QueryType uint8

const (
	QueryList QueryType = 1
	QueryGet  QueryType = 2
	QueryHas  QueryType = 3
)

// QueryPayload is the payload of a MsgQuery RPC.
// Wire format: [1 byte: query type] [8 bytes: request ID] [N bytes: data]
type QueryPayload struct {
	Type      QueryType
	RequestID uint64
	Data      []byte
}

// Encode serializes a QueryPayload into binary.
func (q *QueryPayload) Encode() []byte {
	buf := make([]byte, 1+8+len(q.Data))
	buf[0] = byte(q.Type)
	binary.BigEndian.PutUint64(buf[1:9], q.RequestID)
	copy(buf[9:], q.Data)
	return buf
}

// DecodeQueryPayload deserializes a QueryPayload from binary.
func DecodeQueryPayload(data []byte) (*QueryPayload, error) {
	if len(data) < 9 {
		return nil, fmt.Errorf("query payload too short: %w", syscall.EBADMSG)
	}
	return &QueryPayload{
		Type:      QueryType(data[0]),
		RequestID: binary.BigEndian.Uint64(data[1:9]),
		Data:      data[9:],
	}, nil
}

// QueryResponsePayload is the payload of a MsgQueryResponse RPC.
// Wire format: [8 bytes: request ID] [4 bytes: data len] [N bytes: data] [2 bytes: err len] [M bytes: err]
type QueryResponsePayload struct {
	RequestID uint64
	Data      []byte
	Error     string
}

// Encode serializes a QueryResponsePayload into binary.
func (q *QueryResponsePayload) Encode() []byte {
	errLen := len(q.Error)
	buf := make([]byte, 8+4+len(q.Data)+2+errLen)
	binary.BigEndian.PutUint64(buf[0:8], q.RequestID)
	binary.BigEndian.PutUint32(buf[8:12], uint32(len(q.Data)))
	copy(buf[12:], q.Data)
	off := 12 + len(q.Data)
	binary.BigEndian.PutUint16(buf[off:off+2], uint16(errLen))
	off += 2
	copy(buf[off:], q.Error)
	return buf
}

// DecodeQueryResponsePayload deserializes a QueryResponsePayload from binary.
func DecodeQueryResponsePayload(data []byte) (*QueryResponsePayload, error) {
	if len(data) < 14 {
		return nil, fmt.Errorf("query response payload too short: %w", syscall.EBADMSG)
	}
	reqID := binary.BigEndian.Uint64(data[0:8])
	dataLen := int(binary.BigEndian.Uint32(data[8:12]))
	if 12+dataLen+2 > len(data) {
		return nil, fmt.Errorf("truncated query response data: %w", syscall.EBADMSG)
	}
	respData := make([]byte, dataLen)
	copy(respData, data[12:12+dataLen])
	off := 12 + dataLen
	errLen := int(binary.BigEndian.Uint16(data[off : off+2]))
	off += 2
	if off+errLen > len(data) {
		return nil, fmt.Errorf("truncated query response error: %w", syscall.EBADMSG)
	}
	return &QueryResponsePayload{
		RequestID: reqID,
		Data:      respData,
		Error:     string(data[off : off+errLen]),
	}, nil
}

// LeasePayload is the payload for lease request/grant/release RPCs.
// Wire format: [8 bytes: lease ID] [4 bytes: key len] [N bytes: key] [8 bytes: expiry unixnano]
type LeasePayload struct {
	LeaseID  uint64
	Key      string
	Expiry   int64
}

// Encode serializes a LeasePayload into binary.
func (l *LeasePayload) Encode() []byte {
	keyLen := len(l.Key)
	buf := make([]byte, 8+4+keyLen+8)
	binary.BigEndian.PutUint64(buf[0:8], l.LeaseID)
	binary.BigEndian.PutUint32(buf[8:12], uint32(keyLen))
	copy(buf[12:], l.Key)
	off := 12 + keyLen
	binary.BigEndian.PutUint64(buf[off:off+8], uint64(l.Expiry))
	return buf
}

// DecodeLeasePayload deserializes a LeasePayload from binary.
func DecodeLeasePayload(data []byte) (*LeasePayload, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("lease payload too short: %w", syscall.EBADMSG)
	}
	leaseID := binary.BigEndian.Uint64(data[0:8])
	keyLen := int(binary.BigEndian.Uint32(data[8:12]))
	if 12+keyLen+8 > len(data) {
		return nil, fmt.Errorf("truncated lease payload key: %w", syscall.EBADMSG)
	}
	key := string(data[12 : 12+keyLen])
	off := 12 + keyLen
	expiry := int64(binary.BigEndian.Uint64(data[off : off+8]))
	return &LeasePayload{
		LeaseID: leaseID,
		Key:     key,
		Expiry:  expiry,
	}, nil
}
