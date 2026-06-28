package transport

import (
	"bytes"
	"crypto/subtle"
	"fmt"
	"io"
	"net"
	"strconv"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
)

const hashLength = 64

// MomoTCPCommunicator implements the Communicator interface for the legacy Momo TCP protocol.
type MomoTCPCommunicator struct {
	*common.IdleTimeoutConn
}

// NewMomoTCPCommunicator creates a new MomoTCPCommunicator wrapping a net.Conn.
func NewMomoTCPCommunicator(conn net.Conn) *MomoTCPCommunicator {
	return &MomoTCPCommunicator{
		IdleTimeoutConn: common.NewIdleTimeoutConn(conn, 30*time.Second),
	}
}

func (m *MomoTCPCommunicator) SetAbsoluteDeadline(t interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in SetAbsoluteDeadline: %v: %w", r, syscall.EIO)
		}
	}()
	deadline, ok := t.(time.Time)
	if !ok {
		return fmt.Errorf("invalid deadline type: expected time.Time")
	}
	m.IdleTimeoutConn.SetAbsoluteDeadline(deadline)
	return nil
}

func (m *MomoTCPCommunicator) HandshakeClient(authToken string, timestamp int64, requestedMode int) (mode int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in HandshakeClient: %v: %w", r, syscall.EIO)
		}
	}()

	var handshakeBuf [common.AuthTokenLength + common.TimestampLength + 1]byte
	copy(handshakeBuf[0:common.AuthTokenLength], common.PadString(authToken, common.AuthTokenLength))

	// ⚡ Bolt: Use PadString to ensure the timestamp is exactly 19 bytes and correctly placed.
	copy(handshakeBuf[common.AuthTokenLength:], common.PadString(strconv.FormatInt(timestamp, 10), common.TimestampLength))
	
	// Write the requested mode (1 byte) at the end
	handshakeBuf[common.AuthTokenLength+common.TimestampLength] = byte(requestedMode + '0')

	if _, err := m.Write(handshakeBuf[:]); err != nil {
		return 0, fmt.Errorf("failed to send handshake: %v: %w", err, syscall.EIO)
	}

	var bufferReplicationMode [1]byte
	if _, err := io.ReadFull(io.LimitReader(m, 1), bufferReplicationMode[:]); err != nil {
		return 0, fmt.Errorf("failed to read replication mode response: %v: %w", err, syscall.EBADMSG)
	}

	replicationModeInt64, err := common.SafeParseInt(bufferReplicationMode[:])
	if err != nil {
		return 0, fmt.Errorf("invalid replication mode response: %v: %w", err, syscall.EBADMSG)
	}

	return int(replicationModeInt64), nil
}

func (m *MomoTCPCommunicator) HandshakeServer(expectedAuthToken []byte) (requestedMode int, timestamp int64, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in HandshakeServer: %v: %w", r, syscall.EIO)
		}
	}()

	var handshakeBuf [common.AuthTokenLength + common.TimestampLength + 1]byte
	if _, err := io.ReadFull(io.LimitReader(m, common.AuthTokenLength+common.TimestampLength+1), handshakeBuf[:]); err != nil {
		return 0, 0, fmt.Errorf("failed to read handshake: %v: %w", err, syscall.EBADMSG)
	}

	bufferAuthToken := handshakeBuf[:common.AuthTokenLength]
	bufferTimestamp := handshakeBuf[common.AuthTokenLength : common.AuthTokenLength+common.TimestampLength]
	requestedModeByte := handshakeBuf[common.AuthTokenLength+common.TimestampLength]

	if subtle.ConstantTimeCompare(bufferAuthToken, expectedAuthToken) != 1 {
		return 0, 0, syscall.EACCES
	}

	timestamp, err = common.SafeParseInt(bufferTimestamp)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse timestamp: %v: %w", err, syscall.EBADMSG)
	}

	requestedMode = int(requestedModeByte - '0')
	if requestedMode < 0 || requestedMode > 9 {
		return 0, 0, fmt.Errorf("invalid requested mode: %d: %w", requestedMode, syscall.EBADMSG)
	}

	return requestedMode, timestamp, nil
}

// SendReplicationMode is a helper for HandshakeServer to send the selected mode back.
func (m *MomoTCPCommunicator) SendReplicationMode(mode int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in SendReplicationMode: %v: %w", r, syscall.EIO)
		}
	}()

	var repModeBuf [16]byte
	if _, err := m.Write(strconv.AppendInt(repModeBuf[:0], int64(mode), 10)); err != nil {
		return fmt.Errorf("failed to send replication mode: %v: %w", err, syscall.EIO)
	}
	return nil
}

func (m *MomoTCPCommunicator) SendMetadata(meta *common.FileMetadata) (status int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in SendMetadata: %v: %w", r, syscall.EIO)
		}
	}()

	var metadataBuffer [hashLength + common.FileInfoLength + common.FileInfoLength]byte
	copy(metadataBuffer[0:hashLength], meta.Hash)
	
	wireName := meta.Name
	if meta.RemotePath != "" {
		normalized, normErr := common.NormalizeVirtualPath(meta.RemotePath)
		if normErr != nil {
			return 0, fmt.Errorf("invalid remote path: %w", normErr)
		}
		wireName = normalized + "/" + meta.Name
	}
	if len(wireName) > common.FileInfoLength {
		return 0, fmt.Errorf("metadata name exceeds limit: %w", syscall.ENAMETOOLONG)
	}
	copy(metadataBuffer[hashLength:hashLength+common.FileInfoLength], common.PadString(wireName, common.FileInfoLength))

	var sizeBuf [common.FileInfoLength]byte
	sizeBytes := strconv.AppendInt(sizeBuf[:0], meta.Size, 10)
	copy(metadataBuffer[hashLength+common.FileInfoLength:], sizeBytes)

	if _, err := m.Write(metadataBuffer[:]); err != nil {
		return 0, fmt.Errorf("failed to send metadata: %v: %w", err, syscall.EIO)
	}

	// ⚡ Bolt: Read the metadata status code (1 byte) to determine if we should send the payload.
	var statusBuf [1]byte
	if _, err := io.ReadFull(io.LimitReader(m, 1), statusBuf[:]); err != nil {
		return 0, fmt.Errorf("failed to read metadata status: %v: %w", err, syscall.EBADMSG)
	}
	return int(statusBuf[0]), nil
}

func (m *MomoTCPCommunicator) ReceiveMetadata() (meta common.FileMetadata, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in ReceiveMetadata: %v: %w", r, syscall.EIO)
		}
	}()

	var metadata common.FileMetadata
	var buffer [hashLength + common.FileInfoLength + common.FileInfoLength]byte

	if _, err := io.ReadFull(io.LimitReader(m, hashLength+common.FileInfoLength+common.FileInfoLength), buffer[:]); err != nil {
		return metadata, err
	}

	metadata.Hash = common.SanitizeLog(string(bytesTrimNull(buffer[:hashLength])))
	metadata.Name = string(bytesTrimNull(buffer[hashLength : hashLength+common.FileInfoLength]))

	size, err := common.SafeParseInt(buffer[hashLength+common.FileInfoLength:])
	if err != nil {
		return metadata, err
	}
	metadata.Size = size

	return metadata, nil
}

// SendMetadataStatus is called by the server after receiving metadata.
func (m *MomoTCPCommunicator) SendMetadataStatus(status int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in SendMetadataStatus: %v: %w", r, syscall.EIO)
		}
	}()

	if _, err := m.Write([]byte{byte(status)}); err != nil {
		return fmt.Errorf("failed to send metadata status: %v: %w", err, syscall.EIO)
	}
	return nil
}

// bytesTrimNull is a helper to trim null bytes from a byte slice.
func bytesTrimNull(b []byte) []byte {
	if i := bytes.IndexByte(b, 0); i != -1 {
		return b[:i]
	}
	return b
}

func (m *MomoTCPCommunicator) SendACK(serverId int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in SendACK: %v: %w", r, syscall.EIO)
		}
	}()

	var ackBuf [32]byte
	if _, err := m.Write(strconv.AppendInt(append(ackBuf[:0], "ACK"...), int64(serverId), 10)); err != nil {
		return fmt.Errorf("failed to send ACK: %v: %w", err, syscall.EIO)
	}
	return nil
}

func (m *MomoTCPCommunicator) ReceiveACK() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in ReceiveACK: %v: %w", r, syscall.EIO)
		}
	}()

	var ackBuffer [3]byte
	if _, err := io.ReadFull(io.LimitReader(m, 3), ackBuffer[:]); err != nil {
		return fmt.Errorf("failed to read ACK prefix: %v: %w", err, syscall.EBADMSG)
	}

	if !bytes.Equal(ackBuffer[:], []byte("ACK")) {
		return fmt.Errorf("unexpected response: %s: %w", string(ackBuffer[:]), syscall.EBADMSG)
	}

	// ⚡ Bolt: Read any trailing server ID digits under a short deadline to prevent blocking,
	// limited to at most 10 iterations to prevent infinite-loop CPU exhaustion (DoS).
	m.SetDeadline(time.Now().Add(5 * time.Millisecond))
	var oneByte [1]byte
	for i := 0; i < 10; i++ {
		n, _ := m.Read(oneByte[:])
		if n == 1 && oneByte[0] >= '0' && oneByte[0] <= '9' {
			// Continue
		} else {
			break
		}
	}
	m.SetDeadline(time.Time{}) // Restore default deadline
	return nil
}
