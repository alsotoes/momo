package server

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

func TestWatchSecretsReload(t *testing.T) {
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
	if db != nil {
		defer db.Close()
	}

	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})
	go func() {
		watchSecretsReload(ctx, rm, sigCh)
		close(done)
	}()

	// Wait for the SIGHUP-driven reload to actually fire before cancelling, so
	// the test deterministically exercises the reload branch.
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
