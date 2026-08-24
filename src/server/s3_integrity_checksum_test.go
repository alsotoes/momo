package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"github.com/alsotoes/momo/src/transport"
	"go.uber.org/goleak"
)

// driveS3Put drives a single-part S3 PUT through the exact daemon sequencing
// (HandshakeServer -> SendReplicationMode -> ReceiveMetadata -> getFile),
// returning the server error. On checksum mismatch the S3 BadDigest response is
// written to the peer and getFile returns the error.
func driveS3Put(t *testing.T, auth, name, reqHeader, body string, store storage.Store) (string, error) {
	t.Helper()

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Drive the entire server handshake + getFile in a background goroutine so
	// the net.Pipe client write below never self-deadlocks (both ends of a
	// net.Pipe must be served by different goroutines).
	errCh := make(chan error, 1)
	go func() {
		comm := transport.NewS3Communicator(server)
		comm.SetStore(store)
		expectedAuth := []byte(common.PadString(auth, common.AuthTokenLength))
		if _, _, err := comm.HandshakeServer(expectedAuth); err != nil {
			errCh <- err
			return
		}
		if err := comm.SendReplicationMode(common.ReplicationNone); err != nil {
			errCh <- err
			return
		}
		meta, err := comm.ReceiveMetadata()
		if err != nil {
			errCh <- err
			return
		}
		if err := comm.SendMetadataStatus(transport.MetadataStatusSendPayload); err != nil {
			errCh <- err
			return
		}
		gErr := getFile(comm, store, meta.Name, meta.Hash, meta.Size, "")
		if gErr == nil {
			// Mirrors server.Daemon: persist captured S3 metadata (incl. checksum).
			persistS3Meta(store, meta.Name, meta)
		}
		errCh <- gErr
	}()

	// Write the full request (headers + body) so getFile can consume the payload.
	req := "PUT /" + name + " HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + auth + "\r\n" +
		reqHeader +
		"X-Amz-Content-Sha256: " + sha256Hex(body) + "\r\n" +
		"Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
	if _, err := client.Write([]byte(req)); err != nil {
		t.Fatalf("client write: %v", err)
	}

	// Read the full response (headers + XML error body) to unblock server writes.
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	var sb strings.Builder
	buf := make([]byte, 8192)
	for {
		n, err := client.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String(), <-errCh
}

func TestS3SinglePartChecksum_BadDigest(t *testing.T) {
	defer goleak.VerifyNone(t)
	auth := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	body := "hello integrity"
	name := "checksum/bad.txt"
	header := "X-Amz-Checksum-Sha256: " + base64.StdEncoding.EncodeToString([]byte("garbage")) + "\r\n"

	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	defer store.Close()

	_, gErr := driveS3Put(t, auth, name, header, body, store)
	if gErr == nil {
		t.Fatal("expected BadDigest error for mismatched checksum")
	}
	if _, _, gerr := store.Get(name); gerr != syscall.ENOENT {
		t.Errorf("object %q should not be persisted on checksum mismatch: %v", name, gerr)
	}
}

func TestS3SinglePartChecksum_GoodEcho(t *testing.T) {
	defer goleak.VerifyNone(t)
	auth := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	body := "hello integrity"
	name := "checksum/good.txt"
	sum := sha256.Sum256([]byte(body))
	good := base64.StdEncoding.EncodeToString(sum[:])
	header := "X-Amz-Checksum-Sha256: " + good + "\r\n"

	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	defer store.Close()

	resp, gErr := driveS3Put(t, auth, name, header, body, store)
	if gErr != nil {
		t.Fatalf("expected success, got %v (resp=%q)", gErr, resp)
	}
	rc, meta, err := store.Get(name)
	if err != nil {
		t.Fatalf("object should be stored: %v", err)
	}
	rc.Close()
	if meta.Size != int64(len(body)) {
		t.Errorf("size = %d, want %d", meta.Size, len(body))
	}
	if h := store.GetS3Meta(name)["x-amz-checksum-sha256"]; h != good {
		t.Errorf("checksum not persisted/echoed: got %q, want %q", h, good)
	}
}

func TestS3SinglePartChecksum_UnknownAlgorithmInvalidRequest(t *testing.T) {
	defer goleak.VerifyNone(t)
	auth := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	body := "hello integrity"
	name := "checksum/unknown.txt"
	header := "X-Amz-Checksum-Algorithm: MD5\r\n"

	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	defer store.Close()

	resp, gErr := driveS3Put(t, auth, name, header, body, store)
	if gErr == nil {
		t.Fatal("expected rejection for unknown checksum algorithm")
	}
	if !strings.Contains(resp, "InvalidRequest") {
		t.Errorf("expected InvalidRequest, got resp=%q", resp)
	}
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
