package client

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strconv"
	"syscall"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"go.uber.org/goleak"
)

// startMockGetServer starts a TCP server simulating a Momo GET (Download) endpoint.
func startMockGetServer(t *testing.T, authToken string, status byte, payload []byte) (string, net.Listener) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock GET listener: %v", err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()

				// Read 84-byte handshake
				var hs [common.AuthTokenLength + common.TimestampLength + 1]byte
				if _, err := io.ReadFull(c, hs[:]); err != nil {
					return
				}

				// Read 128-byte GET request
				var req [128]byte
				if _, err := io.ReadFull(c, req[:]); err != nil {
					return
				}

				// Write response: 1-byte status + 64-byte size
				var resp [65]byte
				resp[0] = status
				if status == '0' {
					sizeStr := common.PadString(strconv.Itoa(len(payload)), 64)
					copy(resp[1:65], sizeStr)
					if _, err := c.Write(resp[:]); err != nil {
						return
					}
					// Write payload
					_, _ = c.Write(payload)
				} else {
					copy(resp[1:65], common.PadString("0", 64))
					_, _ = c.Write(resp[:])
				}
			}(conn)
		}
	}()

	return ln.Addr().String(), ln
}

func TestDownloadWithFallback_SingleNodeDownSucceedsOnSibling(t *testing.T) {
	defer goleak.VerifyNone(t)

	expectedData := []byte("hello from healthy replica 1")
	addr1, ln1 := startMockGetServer(t, "testtoken", '0', expectedData)
	defer ln1.Close()

	// Find an unused port that immediately rejects connections
	lnDead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to bind dead listener: %v", err)
	}
	addrDead := lnDead.Addr().String()
	lnDead.Close() // immediately close to guarantee ECONNREFUSED

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			AuthToken:         "testtoken",
			ReplicationFactor: 2,
			Protocol:          "momo-tcp",
		},
		Daemons: []*common.Daemon{
			{Host: addrDead},
			{Host: addr1},
		},
	}

	router := NewPheromoneRouter()
	var dst bytes.Buffer

	// Hash that maps candidates
	hash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	name := "testfile.txt"

	err = DownloadWithFallbackRouter(cfg, name, hash, 0, &dst, router)
	if err != nil {
		t.Fatalf("expected successful fallback, got error: %v", err)
	}

	if !bytes.Equal(dst.Bytes(), expectedData) {
		t.Fatalf("expected %q, got %q", string(expectedData), dst.String())
	}

	// Verify that the failing node received a penalty and the healthy node received a reward
	pDead := router.GetPheromone(0)
	pHealthy := router.GetPheromone(1)
	if pDead >= pHealthy {
		t.Fatalf("expected healthy node to have higher pheromone than dead node (dead=%f, healthy=%f)", pDead, pHealthy)
	}
}

func TestDownloadWithFallback_AllNodesDownFailsWithEHOSTUNREACH(t *testing.T) {
	defer goleak.VerifyNone(t)

	lnDead1, _ := net.Listen("tcp", "127.0.0.1:0")
	addr1 := lnDead1.Addr().String()
	lnDead1.Close()

	lnDead2, _ := net.Listen("tcp", "127.0.0.1:0")
	addr2 := lnDead2.Addr().String()
	lnDead2.Close()

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			AuthToken:         "testtoken",
			ReplicationFactor: 2,
			Protocol:          "momo-tcp",
		},
		Daemons: []*common.Daemon{
			{Host: addr1},
			{Host: addr2},
		},
	}

	router := NewPheromoneRouter()
	var dst bytes.Buffer

	hash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	name := "testfile.txt"

	err := DownloadWithFallbackRouter(cfg, name, hash, 0, &dst, router)
	if err == nil {
		t.Fatal("expected error when all nodes are down, got nil")
	}

	if !errors.Is(err, syscall.EHOSTUNREACH) {
		t.Fatalf("expected error wrapping syscall.EHOSTUNREACH, got: %v", err)
	}
}

func TestDownloadWithFallback_CorruptedReplicaFallsBack(t *testing.T) {
	defer goleak.VerifyNone(t)

	// Node 0 returns '1' (error/ENOENT)
	addr0, ln0 := startMockGetServer(t, "testtoken", '1', nil)
	defer ln0.Close()

	// Node 1 returns '0' (success)
	expectedData := []byte("replica 1 rescued the read")
	addr1, ln1 := startMockGetServer(t, "testtoken", '0', expectedData)
	defer ln1.Close()

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			AuthToken:         "testtoken",
			ReplicationFactor: 2,
			Protocol:          "momo-tcp",
		},
		Daemons: []*common.Daemon{
			{Host: addr0},
			{Host: addr1},
		},
	}

	router := NewPheromoneRouter()
	var dst bytes.Buffer

	hash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	name := "testfile.txt"

	err := DownloadWithFallbackRouter(cfg, name, hash, 0, &dst, router)
	if err != nil {
		t.Fatalf("expected fallback to succeed, got error: %v", err)
	}

	if !bytes.Equal(dst.Bytes(), expectedData) {
		t.Fatalf("expected %q, got %q", string(expectedData), dst.String())
	}
}
