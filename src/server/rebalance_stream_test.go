package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"github.com/alsotoes/momo/src/transport"
)

// streamComm records what a streaming push sends.
type streamComm struct {
	transport.Communicator
	buf      bytes.Buffer
	meta     *common.FileMetadata
	status   int
	writeErr error
	acked    bool
}

func (c *streamComm) SendMetadata(meta *common.FileMetadata) (int, error) {
	c.meta = meta
	if c.status == 0 {
		return transport.MetadataStatusSendPayload, nil
	}
	return c.status, nil
}

func (c *streamComm) Write(p []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return c.buf.Write(p)
}

func (c *streamComm) ReceiveACK() error {
	c.acked = true
	return nil
}

func (c *streamComm) Close() error { return nil }

func streamingPushSource(t *testing.T, status int) (*DaemonRebuildSource, *streamComm, *storage.CASStore, string) {
	t.Helper()
	store, hashes := rebalanceTestStore(t, 1)
	comm := &streamComm{status: status}
	src := &DaemonRebuildSource{store: store}
	src.SetStore(store)
	src.dialFn = func(int) (transport.Communicator, error) { return comm, nil }
	return src, comm, store, hashes[0]
}

func TestPushStreamsFromDisk(t *testing.T) {
	src, comm, _, hash := streamingPushSource(t, 0)
	payload := strings.Repeat("p", 16) + "a" // matches rebalanceTestStore blob 0
	if err := src.Push(context.Background(), hash, 1); err != nil {
		t.Fatalf("push: %v", err)
	}
	if comm.meta == nil || comm.meta.Hash != hash || comm.meta.Size != int64(len(payload)) {
		t.Fatalf("unexpected metadata: %+v", comm.meta)
	}
	if comm.buf.String() != payload {
		t.Fatalf("streamed payload mismatch: %q", comm.buf.String())
	}
	if !comm.acked {
		t.Fatal("push must wait for the peer ack")
	}
}

func TestPushStreamSkipPayloadWhenPeerHasBlob(t *testing.T) {
	src, comm, _, hash := streamingPushSource(t, transport.MetadataStatusSkipPayload)
	if err := src.Push(context.Background(), hash, 1); err != nil {
		t.Fatalf("push: %v", err)
	}
	if comm.buf.Len() != 0 {
		t.Fatal("skip status must not send payload")
	}
}

func TestPushStreamDialFailure(t *testing.T) {
	src, _, _, hash := streamingPushSource(t, 0)
	src.dialFn = func(int) (transport.Communicator, error) { return nil, errors.New("no route") }
	if err := src.Push(context.Background(), hash, 1); err == nil {
		t.Fatal("expected dial error")
	}
}

func TestPushStreamWriteFailure(t *testing.T) {
	src, comm, _, hash := streamingPushSource(t, 0)
	comm.writeErr = errors.New("broken pipe")
	if err := src.Push(context.Background(), hash, 1); err == nil {
		t.Fatal("expected write error")
	}
}

func TestPushStreamMissingBlob(t *testing.T) {
	src, _, _, _ := streamingPushSource(t, 0)
	if err := src.Push(context.Background(), "missing-hash", 1); err == nil {
		t.Fatal("expected read error for a missing blob")
	}
}

func TestPushStreamThrottled(t *testing.T) {
	src, comm, _, hash := streamingPushSource(t, 0)
	src.SetBandwidthLimit(0.5) // 62.5 kB/s
	if err := src.Push(context.Background(), hash, 1); err != nil {
		t.Fatalf("push: %v", err)
	}
	if comm.buf.Len() == 0 {
		t.Fatal("expected payload through the throttled path")
	}
}

func TestWriteThrottledReaderNilLimiterAndErrors(t *testing.T) {
	var buf bytes.Buffer
	if err := writeThrottledReader(context.Background(), &buf, strings.NewReader("hi"), nil); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if buf.String() != "hi" {
		t.Fatalf("got %q", buf.String())
	}
	boom := &failingReader{err: errors.New("disk")}
	if err := writeThrottledReader(context.Background(), io.Discard, boom, newBandwidthLimiter(1000)); err == nil {
		t.Fatal("expected reader error")
	}
}

type failingReader struct{ err error }

func (f *failingReader) Read([]byte) (int, error) { return 0, f.err }
