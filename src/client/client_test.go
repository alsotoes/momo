package client

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"

	momo_common "github.com/alsotoes/momo/src/common"
)

// mockServer is a helper function to create a mock TCP server for testing.
func mockServer(t *testing.T, listener net.Listener, replicationMode int, contentCh chan<- string) {
	t.Helper()
	conn, err := listener.Accept()
	if err != nil {
		t.Logf("Server accept error: %v", err)
		return
	}
	defer conn.Close()

	// Read timestamp. Note: This is brittle as it assumes a fixed length.
	// A real server would have a more robust way to read the initial handshake data.
	buf := make([]byte, 19) 
	_, err = conn.Read(buf)
	if err != nil {
		t.Errorf("Server could not read timestamp: %v", err)
		return
	}

	// Write replication mode
	if _, err := conn.Write([]byte(fmt.Sprintf("%d", replicationMode))); err != nil {
		t.Errorf("Server could not write replication mode: %v", err)
		return
	}

	// The rest of this function mimics the file reception part of the server.
	md5Buffer := make([]byte, 32)
	if _, err := conn.Read(md5Buffer); err != nil {
		// This can fail if the client closes the connection after handshake, which is ok.
		return
	}

	nameBuffer := make([]byte, momo_common.LENGTHINFO)
	if _, err := conn.Read(nameBuffer); err != nil {
		t.Errorf("Server could not read name: %v", err)
		return
	}

	sizeBuffer := make([]byte, momo_common.LENGTHINFO)
	if _, err := conn.Read(sizeBuffer); err != nil {
		t.Errorf("Server could not read size: %v", err)
		return
	}

	fileBuffer := make([]byte, momo_common.BUFFERSIZE)
	n, err := conn.Read(fileBuffer)
	if err != nil {
		t.Errorf("Server could not read file content: %v", err)
		return
	}

	// Send content to the main test goroutine for verification
	contentCh <- string(fileBuffer[:n])

	// Send ACK
	if _, err := conn.Write([]byte("ACK")); err != nil {
		t.Errorf("Server could not write ACK: %v", err)
		return
	}
}

func TestConnect_NoReplication(t *testing.T) {
	content := "hello from client"
	tmpfile, err := ioutil.TempFile("", "test.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	contentCh := make(chan string, 1)
	go mockServer(t, listener, momo_common.NO_REPLICATION, contentCh)

	daemons := []*momo_common.Daemon{
		{Host: listener.Addr().String()},
	}

	Connect(daemons, tmpfile.Name(), 0, time.Now().UnixNano())

	select {
	case receivedContent := <-contentCh:
		if receivedContent != content {
			t.Errorf("Expected content '%s', but got '%s'", content, receivedContent)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timed out waiting for server to receive content")
	}
}

func TestConnect_PrimarySplayReplication(t *testing.T) {
	content := "hello from splay"
	tmpfile, err := ioutil.TempFile("", "test_splay.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	listeners := make([]net.Listener, 3)
	daemons := make([]*momo_common.Daemon, 3)
	contentCh := make(chan string, 3)

	for i := 0; i < 3; i++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		listeners[i] = listener
		daemons[i] = &momo_common.Daemon{Host: listener.Addr().String()}

		replicationMode := momo_common.NO_REPLICATION
		if i == 0 {
			replicationMode = momo_common.PRIMARY_SPLAY_REPLICATION
		}
		go mockServer(t, listeners[i], replicationMode, contentCh)
	}

	Connect(daemons, tmpfile.Name(), 0, time.Now().UnixNano())

	receivedCount := 0
	for i := 0; i < 3; i++ {
		select {
		case receivedContent := <-contentCh:
			if receivedContent != content {
				t.Errorf("Expected content '%s', but got '%s'", content, receivedContent)
			}
			receivedCount++
		case <-time.After(2 * time.Second):
			t.Fatalf("Timed out waiting for server %d to receive content", i)
		}
	}

	if receivedCount != 3 {
		t.Errorf("Expected to receive content on 3 servers, but got %d", receivedCount)
	}
}
