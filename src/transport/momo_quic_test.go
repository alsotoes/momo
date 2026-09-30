package transport

import (
	"errors"
	"net"
	"strings"
	"time"
	"syscall"
	"testing"

	"go.uber.org/goleak"

	"github.com/alsotoes/momo/src/common"
)

func TestMomoQUICReceiveMetadata_RejectsNamePathTraversal(t *testing.T) {
	defer goleak.VerifyNone(t)
	tests := []struct {
		name     string
		fileName string
		wantErr  bool
	}{
		{name: "valid name accepted", fileName: "file.txt"},
		{name: "virtual directory accepted", fileName: "dir/file.txt"},
		{name: "dot-dot traversal rejected", fileName: "..", wantErr: true},
		{name: "relative parent traversal rejected", fileName: "../file.txt", wantErr: true},
		{name: "absolute path rejected", fileName: "/etc/passwd", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buffer [64 + common.FileInfoLength + common.FileInfoLength]byte
			copy(buffer[0:64], common.PadString(strings.Repeat("a", 64), 64))
			copy(buffer[64:64+common.FileInfoLength], common.PadString(tc.fileName, common.FileInfoLength))
			common.WritePaddedInt(buffer[64+common.FileInfoLength:], 42, common.FileInfoLength)

			clientConn, serverConn := net.Pipe()
			defer clientConn.Close()
			defer serverConn.Close()

			go func() {
				clientConn.Write(buffer[:])
			}()

			comm := &MomoQUICCommunicator{
				timeoutConn: common.NewIdleTimeoutConn(serverConn, 5*time.Second),
			}
			_, err := comm.ReceiveMetadata()
			if tc.wantErr {
				if err == nil {
					t.Errorf("ReceiveMetadata() expected error for name %q, got nil", tc.fileName)
				} else if !errors.Is(err, syscall.EBADMSG) {
					t.Errorf("ReceiveMetadata() expected EBADMSG for name %q, got: %v", tc.fileName, err)
				}
			} else if err != nil {
				t.Fatalf("ReceiveMetadata() failed: %v", err)
			}
		})
	}
}
