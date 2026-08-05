package p2p

import (
	"bytes"
	"errors"
	"net"
	"testing"
	"time"
)

var errTestOPRF = errors.New("oprf eval failed")

type mockOPRFEvaluator struct {
	shareIndex int
	eval       []byte
	err        error
}

func (m *mockOPRFEvaluator) ShareIndex() int {
	if m.shareIndex == 0 {
		return 1
	}
	return m.shareIndex
}

func (m *mockOPRFEvaluator) EvaluateShare(shareIndex int, blinded []byte) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.eval, nil
}

func TestOPRFEvalRequestPayload_EncodeDecode(t *testing.T) {
	payload := &OPRFEvalRequestPayload{
		RequestID:  42,
		ShareIndex: 3,
		Blinded:    bytes.Repeat([]byte{0xAB}, 32),
	}
	data := payload.Encode()
	decoded, err := DecodeOPRFEvalRequestPayload(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.RequestID != payload.RequestID {
		t.Errorf("RequestID mismatch")
	}
	if decoded.ShareIndex != payload.ShareIndex {
		t.Errorf("ShareIndex mismatch")
	}
	if !bytes.Equal(decoded.Blinded, payload.Blinded) {
		t.Errorf("Blinded mismatch")
	}
}

func TestOPRFEvalRequestPayload_TooShort(t *testing.T) {
	if _, err := DecodeOPRFEvalRequestPayload([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error for short payload")
	}
}

func TestOPRFEvalResponsePayload_EncodeDecode(t *testing.T) {
	payload := &OPRFEvalResponsePayload{
		RequestID:  7,
		ShareIndex: 2,
		Error:      "boom",
		Eval:       bytes.Repeat([]byte{0xCD}, 32),
	}
	data := payload.Encode()
	decoded, err := DecodeOPRFEvalResponsePayload(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.RequestID != payload.RequestID {
		t.Errorf("RequestID mismatch")
	}
	if decoded.ShareIndex != payload.ShareIndex {
		t.Errorf("ShareIndex mismatch")
	}
	if decoded.Error != payload.Error {
		t.Errorf("Error mismatch")
	}
	if !bytes.Equal(decoded.Eval, payload.Eval) {
		t.Errorf("Eval mismatch")
	}
}

func TestOPRFEvalResponsePayload_EmptyEval(t *testing.T) {
	payload := &OPRFEvalResponsePayload{
		RequestID:  7,
		ShareIndex: 2,
		Error:      "no such share",
	}
	data := payload.Encode()
	decoded, err := DecodeOPRFEvalResponsePayload(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Error != "no such share" {
		t.Errorf("Error mismatch")
	}
	if decoded.Eval != nil {
		t.Errorf("Eval should be nil")
	}
}

func TestOPRFEvalResponsePayload_TooShort(t *testing.T) {
	if _, err := DecodeOPRFEvalResponsePayload([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error for short payload")
	}
}

func TestOPRFProvider_Evaluate(t *testing.T) {
	tr1 := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	tr2 := NewTCPTransport(TCPTransportConfig{LocalID: 2})
	defer tr1.Close()
	defer tr2.Close()

	ln1, _ := net.Listen("tcp", "127.0.0.1:0")
	addr1 := ln1.Addr().String()
	ln1.Close()
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	addr2 := ln2.Addr().String()
	ln2.Close()

	tr1.Listen(addr1)
	tr2.Listen(addr2)

	tr1.Dial(2, addr2)
	time.Sleep(100 * time.Millisecond)

	eval1 := bytes.Repeat([]byte{0x11}, 32)
	eval2 := bytes.Repeat([]byte{0x22}, 32)
	handler1 := &mockOPRFEvaluator{eval: eval1}
	handler2 := &mockOPRFEvaluator{eval: eval2}

	op1 := NewOPRFProvider(1, tr1, handler1)
	op2 := NewOPRFProvider(2, tr2, handler2)

	g1 := NewGossiper(DefaultGossipConfig(1), tr1)
	g2 := NewGossiper(DefaultGossipConfig(2), tr2)
	g1.SetOPRFProvider(op1)
	g2.SetOPRFProvider(op2)
	defer g1.Close()
	defer g2.Close()

	g1.Run()
	g2.Run()

	time.Sleep(200 * time.Millisecond)

	blinded := bytes.Repeat([]byte{0xEE}, 32)
	responses, count := op1.Evaluate(blinded, 2*time.Second)
	if count < 1 {
		t.Fatalf("expected at least 1 response, got %d", count)
	}

	found := false
	for _, resp := range responses {
		if resp.Error == "" && bytes.Equal(resp.Eval, eval2) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find eval2 in responses, got %v", responses)
	}
}

func TestOPRFProvider_EvaluateNoPeers(t *testing.T) {
	tr := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	defer tr.Close()

	handler := &mockOPRFEvaluator{eval: []byte("eval")}
	op := NewOPRFProvider(1, tr, handler)

	responses, count := op.Evaluate([]byte("blinded"), 1*time.Second)
	if count != 0 {
		t.Errorf("expected 0 responses with no peers, got %d", count)
	}
	if responses != nil {
		t.Errorf("expected nil responses, got %v", responses)
	}
}

func TestOPRFProvider_EvaluateWithError(t *testing.T) {
	tr1 := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	tr2 := NewTCPTransport(TCPTransportConfig{LocalID: 2})
	defer tr1.Close()
	defer tr2.Close()

	ln1, _ := net.Listen("tcp", "127.0.0.1:0")
	addr1 := ln1.Addr().String()
	ln1.Close()
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	addr2 := ln2.Addr().String()
	ln2.Close()

	tr1.Listen(addr1)
	tr2.Listen(addr2)

	tr1.Dial(2, addr2)
	time.Sleep(100 * time.Millisecond)

	handler1 := &mockOPRFEvaluator{eval: []byte("ok")}
	handler2 := &mockOPRFEvaluator{err: errTestOPRF}

	op1 := NewOPRFProvider(1, tr1, handler1)
	op2 := NewOPRFProvider(2, tr2, handler2)

	g1 := NewGossiper(DefaultGossipConfig(1), tr1)
	g2 := NewGossiper(DefaultGossipConfig(2), tr2)
	g1.SetOPRFProvider(op1)
	g2.SetOPRFProvider(op2)
	defer g1.Close()
	defer g2.Close()

	g1.Run()
	g2.Run()

	time.Sleep(200 * time.Millisecond)

	responses, count := op1.Evaluate([]byte("blinded"), 2*time.Second)
	if count < 1 {
		t.Fatalf("expected at least 1 response, got %d", count)
	}

	// The failing peer should be present but carry an error.
	sawError := false
	for _, resp := range responses {
		if resp.Error != "" {
			sawError = true
			break
		}
	}
	if !sawError {
		t.Errorf("expected an errored response from the failing peer, got %v", responses)
	}
}
