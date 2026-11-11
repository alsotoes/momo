package main

import (
	"errors"
	"syscall"
	"testing"

	"github.com/alsotoes/momo/src/common"
)

// TestRunRotateSecrets_Refuses verifies the CLI reports that rotation is not
// wired instead of performing a mislabelled provider reload (#1329). It must
// not need a running daemon to say so.
func TestRunRotateSecrets_Refuses(t *testing.T) {
	cfg := common.Configuration{
		Daemons: []*common.Daemon{{}},
	}
	err := runRotateSecrets(cfg, 0)
	if !errors.Is(err, common.ErrRotationNotWired) {
		t.Fatalf("expected ErrRotationNotWired, got %v", err)
	}
	if !errors.Is(err, syscall.ENOTSUP) {
		t.Fatalf("expected ENOTSUP so the CLI exits as a crypto error, got %v", err)
	}
}

func TestRunRotateSecrets_InvalidServerID(t *testing.T) {
	if err := runRotateSecrets(common.Configuration{}, 5); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("expected EINVAL for out-of-range server ID, got %v", err)
	}
}
