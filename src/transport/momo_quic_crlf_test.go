package transport

import (
	"errors"
	"syscall"
	"testing"

	"github.com/alsotoes/momo/src/common"
)

func TestMomoQUICCommunicator_SendMetadataCRLF(t *testing.T) {
	// For SendMetadata validation errors, we just need a non-nil struct so it can do memory validation.
	comm := &MomoQUICCommunicator{}

	// Malicious Name with CRLF
	badMeta := &common.FileMetadata{
		Name: "test\r\nName",
		Hash: "hash123",
		Size: 100,
	}
	_, err := comm.SendMetadata(badMeta)
	if err == nil {
		t.Fatal("Expected SendMetadata to fail with CRLF in name")
	}
	if !errors.Is(err, syscall.EBADMSG) {
		t.Errorf("Expected EBADMSG error, got: %v", err)
	}
}
