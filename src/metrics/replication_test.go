package momo

import (
	"encoding/json"
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
	cfg := momo_common.Configuration{
		Daemons: []*momo_common.Daemon{
			{
				ChangeReplication: serverAddr,
			},
		},
	}

	// Override GetConfigFromFile to return the mock config
	originalGetConfig := momo_common.GetConfigFromFile
	momo_common.GetConfigFromFile = func() (momo_common.Configuration, error) {
		return cfg, nil
	}
	defer func() { momo_common.GetConfigFromFile = originalGetConfig }()

	pushNewReplicationMode(5)

	// Give the server time to process the request
	time.Sleep(100 * time.Millisecond)
}
