package transport

import (
	"fmt"
	"io"
	"syscall"
)

// writeStatusByte writes a single protocol status byte and maps a failed write
// to an EIO-wrapped error. A status response (e.g. LIST/GET/DELETE handshake
// signalling) that silently fails would leave the client blocked waiting for a
// reply it never receives (issue #651).
func writeStatusByte(m io.Writer, status byte) error {
	if _, err := m.Write([]byte{status}); err != nil {
		return fmt.Errorf("failed to send status byte %q: %v: %w", status, err, syscall.EIO)
	}
	return nil
}
