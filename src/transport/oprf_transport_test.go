package transport

import (
	"bytes"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

const testAuthToken = "test-token-11111111111111111111111111111111111111111111111111111" // notsecret

var errOPRFQuorum = errors.New("oprf quorum not met")

type mockOPRFService struct {
	mu      sync.Mutex
	results []OPRFEvalResult
	err     error
	got     []byte
}

func (m *mockOPRFService) EvaluateOPRF(blinded []byte, timeout time.Duration) ([]OPRFEvalResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.got = append([]byte(nil), blinded...)
	if m.err != nil {
		return nil, m.err
	}
	out := make([]OPRFEvalResult, len(m.results))
	copy(out, m.results)
	return out, nil
}

func (m *mockOPRFService) gotBlinded() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.got...)
}

func TestMomoTCP_SendOPRFEval_RoundTrip(t *testing.T) {
	expectedAuthToken := []byte(common.PadString(testAuthToken, common.AuthTokenLength))
	blinded := bytes.Repeat([]byte{0xEE}, 32)
	eval1 := bytes.Repeat([]byte{0x11}, 32)
	eval2 := bytes.Repeat([]byte{0x22}, 32)

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			Protocol:  "momo-tcp",
			AuthToken: testAuthToken,
		},
	}
	factory := NewProtocolFactory(cfg)

	l, err := factory.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer l.Close()

	svc := &mockOPRFService{
		results: []OPRFEvalResult{
			{ShareIndex: 1, Eval: eval1},
			{ShareIndex: 2, Eval: eval2},
		},
	}

	// Server side.
	go func() {
		comm, aerr := l.Accept()
		if aerr != nil {
			return
		}
		defer comm.Close()
		if sc, ok := comm.(interface{ SetOPRFService(OPRFService) }); ok {
			sc.SetOPRFService(svc)
		}
		// The gateway fully handles ModeOPRFEval internally and returns
		// ErrRequestHandled; the client-side assertions below are the source
		// of truth.
		_, _, _ = comm.HandshakeServer(expectedAuthToken)
	}()

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	comm := NewMomoTCPCommunicator(conn)
	defer comm.Close()

	results, err := comm.SendOPRFEval(testAuthToken, time.Now().UnixNano(), blinded, 2)
	if err != nil {
		t.Fatalf("SendOPRFEval failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !bytes.Equal(svc.gotBlinded(), blinded) {
		t.Errorf("server received wrong blinded tag: %x", svc.gotBlinded())
	}
	if results[0].ShareIndex != 1 || !bytes.Equal(results[0].Eval, eval1) {
		t.Errorf("result[0] mismatch: %+v", results[0])
	}
	if results[1].ShareIndex != 2 || !bytes.Equal(results[1].Eval, eval2) {
		t.Errorf("result[1] mismatch: %+v", results[1])
	}
}

func TestMomoTCP_SendOPRFEval_QuorumNotMet(t *testing.T) {
	expectedAuthToken := []byte(common.PadString(testAuthToken, common.AuthTokenLength))
	blinded := bytes.Repeat([]byte{0xEE}, 32)

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			Protocol:  "momo-tcp",
			AuthToken: testAuthToken,
		},
	}
	factory := NewProtocolFactory(cfg)

	l, err := factory.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer l.Close()

	svc := &mockOPRFService{err: errOPRFQuorum}

	go func() {
		comm, aerr := l.Accept()
		if aerr != nil {
			return
		}
		defer comm.Close()
		if sc, ok := comm.(interface{ SetOPRFService(OPRFService) }); ok {
			sc.SetOPRFService(svc)
		}
		_, _, _ = comm.HandshakeServer(expectedAuthToken)
	}()

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	comm := NewMomoTCPCommunicator(conn)
	defer comm.Close()

	// threshold demand exceeds what the server can supply -> fail closed.
	if _, err := comm.SendOPRFEval(testAuthToken, time.Now().UnixNano(), blinded, 3); err == nil {
		t.Fatal("expected SendOPRFEval to fail when quorum is not met")
	}
}

func TestMomoTCP_SendOPRFEval_RejectsBadBlinded(t *testing.T) {
	conn, _ := net.Pipe()
	defer conn.Close()
	comm := NewMomoTCPCommunicator(conn)

	if _, err := comm.SendOPRFEval(testAuthToken, time.Now().UnixNano(), []byte("short"), 1); err == nil {
		t.Fatal("expected SendOPRFEval to reject a non-32-byte blinded tag")
	}
}

func TestMomoQUIC_SendOPRFEval_RoundTrip(t *testing.T) {
	expectedAuthToken := []byte(common.PadString(testAuthToken, common.AuthTokenLength))
	blinded := bytes.Repeat([]byte{0xDD}, 32)
	eval1 := bytes.Repeat([]byte{0xAA}, 32)

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			Protocol:    "momo-quic",
			AuthToken:   testAuthToken,
			TLSInsecure: true,
		},
	}
	factory := NewProtocolFactory(cfg)

	l, err := factory.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer l.Close()

	svc := &mockOPRFService{
		results: []OPRFEvalResult{{ShareIndex: 1, Eval: eval1}},
	}

	// Server side.
	go func() {
		comm, aerr := l.Accept()
		if aerr != nil {
			return
		}
		defer comm.Close()
		if sc, ok := comm.(interface{ SetOPRFService(OPRFService) }); ok {
			sc.SetOPRFService(svc)
		}
		_, _, _ = comm.HandshakeServer(expectedAuthToken)
	}()

	clientComm, err := factory.Dial(l.Addr().String())
	if err != nil {
		t.Fatalf("Client failed to dial: %v", err)
	}
	defer clientComm.Close()

	results, err := clientComm.SendOPRFEval(testAuthToken, time.Now().UnixNano(), blinded, 1)
	if err != nil {
		t.Fatalf("SendOPRFEval failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !bytes.Equal(svc.gotBlinded(), blinded) {
		t.Errorf("server received wrong blinded tag: %x", svc.gotBlinded())
	}
	if results[0].ShareIndex != 1 || !bytes.Equal(results[0].Eval, eval1) {
		t.Errorf("result mismatch: %+v", results[0])
	}
}
