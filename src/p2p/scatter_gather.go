package p2p

import (
	"log"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// QueryHandler processes a scatter-gather query locally and returns the result.
type QueryHandler interface {
	HandleQuery(qt QueryType, data []byte) ([]byte, error)
}

// ScatterGather provides distributed query capabilities over the P2P transport.
// It broadcasts a query to all alive peers and collects their responses within
// a timeout window. RPCs are routed by the Gossiper's consumer loop via HandleRPC.
type ScatterGather struct {
	localID   int32
	transport Transport
	handler   QueryHandler

	nextRequestID atomic.Uint64
	pendingMu     sync.Mutex
	pending       map[uint64]*pendingQuery
}

type pendingQuery struct {
	responses chan QueryResponsePayload
	peerCount int
}

// NewScatterGather creates a new ScatterGather instance.
// The handler is invoked for incoming queries from remote peers.
func NewScatterGather(localID int32, transport Transport, handler QueryHandler) *ScatterGather {
	return &ScatterGather{
		localID:   localID,
		transport: transport,
		handler:   handler,
		pending:   make(map[uint64]*pendingQuery),
	}
}

// HandleRPC dispatches query-related RPCs. Called by the Gossiper's consumer loop.
func (sg *ScatterGather) HandleRPC(rpc *RPC) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("ScatterGather HandleRPC panic recovered: %v (errno=%d)", r, syscall.EIO)
		}
	}()
	switch rpc.Type {
	case MsgQuery:
		sg.handleQuery(rpc)
	case MsgQueryResponse:
		sg.handleQueryResponse(rpc)
	}
}

// handleQuery processes an incoming query from a remote peer, invokes the local
// handler, and sends the response back.
func (sg *ScatterGather) handleQuery(rpc *RPC) {
	payload, err := DecodeQueryPayload(rpc.Payload)
	if err != nil {
		log.Printf("ScatterGather: failed to decode query from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}

	var respData []byte
	var respErr string
	if sg.handler != nil {
		data, err := sg.handler.HandleQuery(payload.Type, payload.Data)
		if err != nil {
			respErr = err.Error()
		} else {
			respData = data
		}
	}

	resp := &QueryResponsePayload{
		RequestID: payload.RequestID,
		Data:      respData,
		Error:     respErr,
	}

	respRPC := &RPC{
		From:    sg.localID,
		Type:    MsgQueryResponse,
		Payload: resp.Encode(),
	}

	if err := sg.transport.Send(rpc.From, respRPC); err != nil {
		log.Printf("ScatterGather: failed to send response to peer %d: %v (errno=%d)", rpc.From, err, syscall.EHOSTUNREACH)
	}
}

// handleQueryResponse routes an incoming query response to the pending request.
func (sg *ScatterGather) handleQueryResponse(rpc *RPC) {
	payload, err := DecodeQueryResponsePayload(rpc.Payload)
	if err != nil {
		log.Printf("ScatterGather: failed to decode query response from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}

	sg.pendingMu.Lock()
	pq, ok := sg.pending[payload.RequestID]
	sg.pendingMu.Unlock()

	if !ok {
		return
	}

	select {
	case pq.responses <- *payload:
	default:
	}
}

// Query broadcasts a query to all alive peers and collects responses within
// the given timeout. Returns the collected responses and the number of peers
// that responded.
func (sg *ScatterGather) Query(qt QueryType, data []byte, timeout time.Duration) ([]QueryResponsePayload, int) {
	peers := sg.transport.Peers().Alive()
	peerCount := 0
	for _, p := range peers {
		if p.ID != sg.localID {
			peerCount++
		}
	}
	if peerCount == 0 {
		return nil, 0
	}

	requestID := sg.nextRequestID.Add(1)
	payload := &QueryPayload{
		Type:      qt,
		RequestID: requestID,
		Data:      data,
	}

	rpc := &RPC{
		From:    sg.localID,
		Type:    MsgQuery,
		Payload: payload.Encode(),
	}

	pq := &pendingQuery{
		responses: make(chan QueryResponsePayload, peerCount),
		peerCount: peerCount,
	}

	sg.pendingMu.Lock()
	sg.pending[requestID] = pq
	sg.pendingMu.Unlock()

	defer func() {
		sg.pendingMu.Lock()
		delete(sg.pending, requestID)
		sg.pendingMu.Unlock()
	}()

	sg.transport.Broadcast(rpc)

	var results []QueryResponsePayload
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for len(results) < peerCount {
		select {
		case resp := <-pq.responses:
			results = append(results, resp)
		case <-timer.C:
			return results, len(results)
		}
	}

	return results, len(results)
}
