package server

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

func newTestRotationManager(t *testing.T) (*common.RotationManager, func()) {
	t.Helper()
	cfg := common.Configuration{
		Secrets: common.SecretsConfig{
			Enabled: true,
			Sources: []common.SecretsSource{common.SecretsSourceEnv},
		},
	}
	rm, db := initSecretsManager(context.Background(), cfg, t.TempDir())
	if rm == nil {
		t.Fatal("expected a rotation manager")
	}
	return rm, func() {
		if db != nil {
			db.Close()
		}
	}
}

func TestWatchSecretsReload(t *testing.T) {
	rm, cleanup := newTestRotationManager(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})
	go func() {
		watchSecretsReload(ctx, rm, sigCh)
		close(done)
	}()

	reloaded := make(chan struct{}, 1)
	rm.RegisterReloadHook(func() {
		select {
		case reloaded <- struct{}{}:
		default:
		}
	})
	sigCh <- syscall.SIGHUP
	select {
	case <-reloaded:
	case <-time.After(time.Second):
		t.Fatal("SIGHUP did not trigger a reload")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchSecretsReload did not stop after cancel")
	}
}

func TestStartSecretsReloadWatcher_Nil(t *testing.T) {
	// Must return immediately when secrets management is disabled.
	done := make(chan struct{})
	go func() {
		startSecretsReloadWatcher(context.Background(), nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("nil watcher should return immediately")
	}
}

func TestStartSecretsReloadWatcher_SIGHUP(t *testing.T) {
	rm, cleanup := newTestRotationManager(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reloaded := make(chan struct{}, 1)
	rm.RegisterReloadHook(func() {
		select {
		case reloaded <- struct{}{}:
		default:
		}
	})

	go startSecretsReloadWatcher(ctx, rm)
	// Give the watcher time to install the SIGHUP handler before signalling.
	time.Sleep(100 * time.Millisecond)

	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("kill SIGHUP: %v", err)
	}
	select {
	case <-reloaded:
	case <-time.After(2 * time.Second):
		t.Fatal("real SIGHUP did not trigger a reload")
	}
}

func TestWatchSecretsReload_ReloadError(t *testing.T) {
	// A rotation manager with a nil provider makes Reload fail (recovered
	// panic); the loop must log and continue, then stop on cancel.
	rm := common.NewRotationManager(nil, nil, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})
	go func() {
		watchSecretsReload(ctx, rm, sigCh)
		close(done)
	}()
	sigCh <- syscall.SIGHUP
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchSecretsReload did not stop")
	}
}
