package p2p

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestQueryPayload_EncodeDecode(t *testing.T) {
	original := &QueryPayload{
		Type:      QueryList,
		RequestID: 12345,
		Data:      []byte("test-data"),
	}

	encoded := original.Encode()
	decoded, err := DecodeQueryPayload(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Type != original.Type {
		t.Errorf("type mismatch: got %d, want %d", decoded.Type, original.Type)
	}
	if decoded.RequestID != original.RequestID {
		t.Errorf("requestID mismatch: got %d, want %d", decoded.RequestID, original.RequestID)
	}
	if string(decoded.Data) != string(original.Data) {
		t.Errorf("data mismatch: got %q, want %q", decoded.Data, original.Data)
	}
}

func TestQueryResponsePayload_EncodeDecode(t *testing.T) {
	original := &QueryResponsePayload{
		RequestID: 99999,
		Data:      []byte("response-data"),
		Error:     "test error",
	}

	encoded := original.Encode()
	decoded, err := DecodeQueryResponsePayload(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.RequestID != original.RequestID {
		t.Errorf("requestID mismatch: got %d, want %d", decoded.RequestID, original.RequestID)
	}
	if string(decoded.Data) != string(original.Data) {
		t.Errorf("data mismatch: got %q, want %q", decoded.Data, original.Data)
	}
	if decoded.Error != original.Error {
		t.Errorf("error mismatch: got %q, want %q", decoded.Error, original.Error)
	}
}

func TestLeasePayload_EncodeDecode(t *testing.T) {
	original := &LeasePayload{
		LeaseID: 42,
		Key:     "file-hash-abc",
		Expiry:  time.Now().UnixNano(),
	}

	encoded := original.Encode()
	decoded, err := DecodeLeasePayload(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.LeaseID != original.LeaseID {
		t.Errorf("leaseID mismatch: got %d, want %d", decoded.LeaseID, original.LeaseID)
	}
	if decoded.Key != original.Key {
		t.Errorf("key mismatch: got %q, want %q", decoded.Key, original.Key)
	}
	if decoded.Expiry != original.Expiry {
		t.Errorf("expiry mismatch: got %d, want %d", decoded.Expiry, original.Expiry)
	}
}

// mockQueryHandler is a test QueryHandler that returns predefined data.
type mockQueryHandler struct {
	data []byte
	err  error
}

func (m *mockQueryHandler) HandleQuery(qt QueryType, data []byte) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.data, nil
}

func TestScatterGather_Query(t *testing.T) {
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

	handler1 := &mockQueryHandler{data: []byte("node1-data")}
	handler2 := &mockQueryHandler{data: []byte("node2-data")}

	sg1 := NewScatterGather(1, tr1, handler1)
	sg2 := NewScatterGather(2, tr2, handler2)

	g1 := NewGossiper(DefaultGossipConfig(1), tr1)
	g2 := NewGossiper(DefaultGossipConfig(2), tr2)
	g1.SetScatterGather(sg1)
	g2.SetScatterGather(sg2)
	defer g1.Close()
	defer g2.Close()

	g1.Run()
	g2.Run()

	time.Sleep(200 * time.Millisecond)

	responses, count := sg1.Query(QueryList, nil, 2*time.Second)
	if count < 1 {
		t.Fatalf("expected at least 1 response, got %d", count)
	}

	found := false
	for _, resp := range responses {
		if string(resp.Data) == "node2-data" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find 'node2-data' in responses, got %v", responses)
	}
}

func TestScatterGather_QueryNoPeers(t *testing.T) {
	tr := NewTCPTransport(TCPTransportConfig{LocalID: 1})
	defer tr.Close()

	handler := &mockQueryHandler{data: []byte("data")}
	sg := NewScatterGather(1, tr, handler)

	responses, count := sg.Query(QueryList, nil, 1*time.Second)
	if count != 0 {
		t.Errorf("expected 0 responses with no peers, got %d", count)
	}
	if responses != nil {
		t.Errorf("expected nil responses, got %v", responses)
	}
}

func TestScatterGather_QueryWithError(t *testing.T) {
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

	handler1 := &mockQueryHandler{data: []byte("ok")}
	handler2 := &mockQueryHandler{err: errTestQuery}

	sg1 := NewScatterGather(1, tr1, handler1)
	sg2 := NewScatterGather(2, tr2, handler2)

	g1 := NewGossiper(DefaultGossipConfig(1), tr1)
	g2 := NewGossiper(DefaultGossipConfig(2), tr2)
	g1.SetScatterGather(sg1)
	g2.SetScatterGather(sg2)
	defer g1.Close()
	defer g2.Close()

	g1.Run()
	g2.Run()

	time.Sleep(200 * time.Millisecond)

	responses, count := sg1.Query(QueryGet, nil, 2*time.Second)
	if count < 1 {
		t.Fatalf("expected at least 1 response, got %d", count)
	}

	for _, resp := range responses {
		if resp.Error != "" {
			return
		}
	}
	t.Errorf("expected at least one error response")
}

var errTestQuery = newTestError("test query error")

func newTestError(msg string) error {
	return &testError{msg: msg}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

func TestScatterGather_LargeData(t *testing.T) {
	largeData := make([]byte, 10000)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	original := &QueryResponsePayload{
		RequestID: 1,
		Data:      largeData,
	}
	encoded := original.Encode()
	decoded, err := DecodeQueryResponsePayload(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(decoded.Data) != len(largeData) {
		t.Errorf("data length mismatch: got %d, want %d", len(decoded.Data), len(largeData))
	}
	for i := range largeData {
		if decoded.Data[i] != largeData[i] {
			t.Errorf("data mismatch at index %d", i)
			break
		}
	}
}

func TestQueryPayload_EmptyData(t *testing.T) {
	original := &QueryPayload{
		Type:      QueryHas,
		RequestID: 1,
		Data:      nil,
	}
	encoded := original.Encode()
	decoded, err := DecodeQueryPayload(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Type != QueryHas {
		t.Errorf("type mismatch")
	}
	if len(decoded.Data) != 0 {
		t.Errorf("expected empty data")
	}
}

func TestLeasePayload_ZeroExpiry(t *testing.T) {
	original := &LeasePayload{
		LeaseID: 1,
		Key:     "test",
		Expiry:  0,
	}
	encoded := original.Encode()
	decoded, err := DecodeLeasePayload(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Expiry != 0 {
		t.Errorf("expected zero expiry")
	}
}

func TestQueryResponseBinaryFormat(t *testing.T) {
	resp := &QueryResponsePayload{
		RequestID: 0xABCDEF,
		Data:      []byte("x"),
		Error:     "e",
	}
	encoded := resp.Encode()

	reqID := binary.BigEndian.Uint64(encoded[0:8])
	if reqID != 0xABCDEF {
		t.Errorf("requestID mismatch: got %x, want %x", reqID, 0xABCDEF)
	}
}
