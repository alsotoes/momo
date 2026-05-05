package metrics

import (
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	momo_common "github.com/alsotoes/momo/src/common"
)

func TestPushNewReplicationMode(t *testing.T) {
	// Create a mock TCP server
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	serverAddr := l.Addr().String()

	go func() {
		fd, err := l.Accept()
		if err != nil {
			t.Logf("Accept error: %v", err)
			return
		}
		defer fd.Close()

		// Read and validate the AuthToken
		bufferAuthToken := make([]byte, momo_common.AuthTokenLength)
		if _, err := io.ReadFull(fd, bufferAuthToken); err != nil {
			t.Logf("Error reading AuthToken: %v", err)
			return
		}

		decoder := json.NewDecoder(fd)
		var data momo_common.ReplicationData
		if err := decoder.Decode(&data); err != nil {
			t.Logf("Decode error: %v", err)
			return
		}

		if data.New != 5 {
			t.Errorf("Expected replication mode 5, got %d", data.New)
		}
	}()

	// Mock config
	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"
	cfg := momo_common.Configuration{
		Daemons: []*momo_common.Daemon{
			{
				ChangeReplication: serverAddr,
			},
		},
		Global: momo_common.ConfigurationGlobal{
			AuthToken: authToken,
		},
	}

	pushNewReplicationMode(cfg, 5)

	// Give the server time to process the request
	time.Sleep(100 * time.Millisecond)
}
