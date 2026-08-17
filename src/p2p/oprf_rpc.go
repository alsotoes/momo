package p2p

import (
	"log"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// OPRFEvaluator evaluates a blinded OPRF point with a daemon's key share.
// Implementations must never log or persist the unblinded input.
type OPRFEvaluator interface {
	// ShareIndex returns this daemon's Shamir evaluation point (daemon index + 1).
	// It labels every evaluation response so the requester can interpolate.
	ShareIndex() int
	// EvaluateShare returns the share evaluation for the given blinded point.
	EvaluateShare(shareIndex int, blinded []byte) ([]byte, error)
}

// OPRFProvider provides distributed threshold-OPRF evaluation over the P2P
// transport. A coordinator asks its peers (and itself) to evaluate a blinded
// dedup tag; the requester combines enough distinct share evaluations to
// derive a content key. Evaluations are collected within a timeout window.
// RPCs are routed by the Gossiper's consumer loop via HandleRPC.
type OPRFProvider struct {
	localID   int32
	transport Transport
	handler   OPRFEvaluator

	nextRequestID atomic.Uint64
	pendingMu     sync.Mutex
	pending       map[uint64]*pendingOPRF
}

type pendingOPRF struct {
	responses chan OPRFEvalResponsePayload
	peerCount int
}

// NewOPRFProvider creates a new OPRFProvider.
// The handler is invoked for incoming OPRF evaluation requests from remote peers.
func NewOPRFProvider(localID int32, transport Transport, handler OPRFEvaluator) *OPRFProvider {
	return &OPRFProvider{
		localID:   localID,
		transport: transport,
		handler:   handler,
		pending:   make(map[uint64]*pendingOPRF),
	}
}

// HandleRPC dispatches OPRF RPCs. Called by the Gossiper's consumer loop.
func (o *OPRFProvider) HandleRPC(rpc *RPC) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("OPRFProvider HandleRPC panic recovered: %v (errno=%d)", r, syscall.EIO)
		}
	}()
	switch rpc.Type {
	case MsgOPRFEvalRequest:
		o.handleEvalRequest(rpc)
	case MsgOPRFEvalResponse:
		o.handleEvalResponse(rpc)
	}
}

// handleEvalRequest processes an incoming OPRF evaluation request, invokes the
// local evaluator, and sends the response back to the requesting peer.
func (o *OPRFProvider) handleEvalRequest(rpc *RPC) {
	payload, err := DecodeOPRFEvalRequestPayload(rpc.Payload)
	if err != nil {
		log.Printf("OPRFProvider: failed to decode eval request from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}

	var eval []byte
	var evalErr string
	if o.handler != nil {
		e, err := o.handler.EvaluateShare(int(payload.ShareIndex), payload.Blinded)
		if err != nil {
			evalErr = err.Error()
		} else {
			eval = e
		}
	} else {
		evalErr = "oprf evaluator not configured"
	}

	// The response is labelled with THIS daemon's own share index, not the one
	// echoed in the request (the coordinator broadcasts one request to all
	// peers and cannot know each peer's index in advance).
	var ownIndex int
	if o.handler != nil {
		ownIndex = o.handler.ShareIndex()
	}

	resp := &OPRFEvalResponsePayload{
		RequestID:  payload.RequestID,
		ShareIndex: byte(ownIndex),
		Error:      evalErr,
		Eval:       eval,
	}

	respRPC := &RPC{
		From:    o.localID,
		Type:    MsgOPRFEvalResponse,
		Payload: resp.Encode(),
	}

	if err := o.transport.Send(rpc.From, respRPC); err != nil {
		log.Printf("OPRFProvider: failed to send response to peer %d: %v (errno=%d)", rpc.From, err, syscall.EHOSTUNREACH)
	}
}

// handleEvalResponse routes an incoming OPRF evaluation response to the pending request.
func (o *OPRFProvider) handleEvalResponse(rpc *RPC) {
	payload, err := DecodeOPRFEvalResponsePayload(rpc.Payload)
	if err != nil {
		log.Printf("OPRFProvider: failed to decode eval response from peer %d: %v (errno=%d)", rpc.From, err, syscall.EBADMSG)
		return
	}

	o.pendingMu.Lock()
	pq, ok := o.pending[payload.RequestID]
	o.pendingMu.Unlock()

	if !ok {
		return
	}

	select {
	case pq.responses <- *payload:
	default:
	}
}

// Evaluate broadcasts a blinded OPRF point to all alive peers and collects their
// share evaluations within the given timeout. It returns the collected responses
// and the number of peers that responded. The caller is responsible for combining
// enough distinct evaluations to meet the configured threshold.
func (o *OPRFProvider) Evaluate(blinded []byte, timeout time.Duration) ([]OPRFEvalResponsePayload, int) {
	// Quality-aware: prefer low-RTT alive peers for evaluation (issue #823).
	peers := o.transport.Peers().AliveByQuality()
	peerCount := 0
	for _, p := range peers {
		if p.ID != o.localID {
			peerCount++
		}
	}
	if peerCount == 0 {
		return nil, 0
	}

	requestID := o.nextRequestID.Add(1)
	payload := &OPRFEvalRequestPayload{
		RequestID: requestID,
		Blinded:   blinded,
	}

	rpc := &RPC{
		From:    o.localID,
		Type:    MsgOPRFEvalRequest,
		Payload: payload.Encode(),
	}

	pq := &pendingOPRF{
		responses: make(chan OPRFEvalResponsePayload, peerCount),
		peerCount: peerCount,
	}

	o.pendingMu.Lock()
	o.pending[requestID] = pq
	o.pendingMu.Unlock()

	defer func() {
		o.pendingMu.Lock()
		delete(o.pending, requestID)
		o.pendingMu.Unlock()
	}()

	o.transport.Broadcast(rpc)

	var results []OPRFEvalResponsePayload
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
