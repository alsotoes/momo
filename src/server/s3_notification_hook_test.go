package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/storage"
	"github.com/alsotoes/momo/src/transport"
)

// TestAfterS3ObjectWriteBucket proves the ingest hook records a version when the
// bucket is versioned and delivers an ObjectCreated webhook (R10, #938).
func TestAfterS3ObjectWriteBucket(t *testing.T) {
	received := make(chan map[string]any, 1)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err == nil {
			select {
			case received <- doc:
			default:
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	bucket := "b"
	if err := store.SetBucketVersioning(bucket, "Enabled"); err != nil {
		t.Fatalf("SetBucketVersioning: %v", err)
	}
	if err := store.SetBucketNotification(bucket, storage.NotificationConfig{Rules: []storage.NotificationRule{{
		ID: "r1", Status: "Enabled",
		Events:  []storage.EventType{storage.EventObjectCreatedPut},
		HTTPUrl: hook.URL,
	}}}); err != nil {
		t.Fatalf("SetBucketNotification: %v", err)
	}

	// retryMax=1 keeps the delivery synchronous and sleep-free.
	afterS3ObjectWriteBucket(store, bucket, "obj/key", "deadbeef", 42, 1)

	if _, ok, verr := store.LatestVersion(bucket, "obj/key"); verr != nil || !ok {
		t.Fatalf("expected a version to be recorded: ok=%v err=%v", ok, verr)
	}

	select {
	case doc := <-received:
		recs, _ := doc["Records"].([]any)
		if len(recs) != 1 {
			t.Fatalf("expected 1 record, got %v", doc)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook was never delivered")
	}
}

// TestAfterS3ObjectWriteBucketSkipsFlatNamespace proves the hook is inert in
// legacy flat-namespace mode (no bucket configured).
func TestAfterS3ObjectWriteBucketSkipsFlatNamespace(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Must not panic or record anything for an empty bucket.
	afterS3ObjectWriteBucket(store, "", "obj/key", "deadbeef", 42, 1)
	if _, ok, _ := store.LatestVersion("", "obj/key"); ok {
		t.Fatal("no version should be recorded for an empty bucket")
	}
}

// TestDispatchObjectEventInertWithoutSupport proves the dispatch helper is a
// no-op for a store without notification support.
func TestDispatchObjectEventInertWithoutSupport(t *testing.T) {
	dispatchObjectEvent(nil, "b", "k", "", storage.EventObjectCreatedPut, 1)
}

// fakeComm is a transport.Communicator stub for the ingest hook: it satisfies
// the interface by embedding it and overrides only the two methods the hook uses.
type fakeComm struct {
	transport.Communicator
	external bool
	bucket   string
}

func (f fakeComm) IsExternalClient() bool   { return f.external }
func (f fakeComm) ConfiguredBucket() string { return f.bucket }

// TestAfterS3ObjectWrite proves the hook is gated on external S3 clients.
func TestAfterS3ObjectWrite(t *testing.T) {
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.SetBucketVersioning("b", "Enabled"); err != nil {
		t.Fatalf("SetBucketVersioning: %v", err)
	}

	// Native (non-external) client: no side effects.
	afterS3ObjectWrite(fakeComm{external: false, bucket: "b"}, store, "native", "h", 1, 1)
	if _, ok, _ := store.LatestVersion("b", "native"); ok {
		t.Fatal("native write must not record a version")
	}

	// External client: delegates to the bucket hook.
	afterS3ObjectWrite(fakeComm{external: true, bucket: "b"}, store, "ext", "h", 1, 1)
	if _, ok, _ := store.LatestVersion("b", "ext"); !ok {
		t.Fatal("external write must record a version")
	}
}
