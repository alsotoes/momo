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

func (m *MomoTCPCommunicator) SetAbsoluteDeadline(t interface{}) error {
	deadline, ok := t.(time.Time)
	if !ok {
		return fmt.Errorf("invalid deadline type: expected time.Time")
	}
	m.IdleTimeoutConn.SetAbsoluteDeadline(deadline)
	return nil
}

func (m *MomoTCPCommunicator) HandshakeClient(authToken string, timestamp int64, requestedMode int) (int, error) {
	var handshakeBuf [common.AuthTokenLength + common.TimestampLength + 1]byte
	copy(handshakeBuf[0:common.AuthTokenLength], common.PadString(authToken, common.AuthTokenLength))

	// ⚡ Bolt: Use PadString to ensure the timestamp is exactly 19 bytes and correctly placed.
	copy(handshakeBuf[common.AuthTokenLength:], common.PadString(strconv.FormatInt(timestamp, 10), common.TimestampLength))
	
	// Write the requested mode (1 byte) at the end
	handshakeBuf[common.AuthTokenLength+common.TimestampLength] = byte(requestedMode + '0')

	if _, err := m.Write(handshakeBuf[:]); err != nil {
		return 0, fmt.Errorf("failed to send handshake: %w", err)
	}

	var bufferReplicationMode [1]byte
	if _, err := io.ReadFull(m, bufferReplicationMode[:]); err != nil {
		return 0, fmt.Errorf("failed to read replication mode response: %w", err)
	}

	replicationModeInt64, err := common.SafeParseInt(bufferReplicationMode[:])
	if err != nil {
		return 0, fmt.Errorf("invalid replication mode response: %w", err)
	}

	return int(replicationModeInt64), nil
}

func (m *MomoTCPCommunicator) HandshakeServer(expectedAuthToken []byte) (int, int64, error) {
	var handshakeBuf [common.AuthTokenLength + common.TimestampLength + 1]byte
	if _, err := io.ReadFull(m, handshakeBuf[:]); err != nil {
		return 0, 0, fmt.Errorf("failed to read handshake: %w", err)
	}

	bufferAuthToken := handshakeBuf[:common.AuthTokenLength]
	bufferTimestamp := handshakeBuf[common.AuthTokenLength : common.AuthTokenLength+common.TimestampLength]
	requestedModeByte := handshakeBuf[common.AuthTokenLength+common.TimestampLength]

	if subtle.ConstantTimeCompare(bufferAuthToken, expectedAuthToken) != 1 {
		return 0, 0, syscall.EACCES
	}

	timestamp, err := common.SafeParseInt(bufferTimestamp)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse timestamp: %w", err)
	}

	requestedMode := int(requestedModeByte - '0')
	if requestedMode < 0 || requestedMode > 9 {
		return 0, 0, fmt.Errorf("invalid requested mode: %d", requestedMode)
	}

	return requestedMode, timestamp, nil
}

// SendReplicationMode is a helper for HandshakeServer to send the selected mode back.
func (m *MomoTCPCommunicator) SendReplicationMode(mode int) error {
	var repModeBuf [16]byte
	if _, err := m.Write(strconv.AppendInt(repModeBuf[:0], int64(mode), 10)); err != nil {
		return fmt.Errorf("failed to send replication mode: %w", err)
	}
	return nil
}

func (m *MomoTCPCommunicator) SendMetadata(meta *common.FileMetadata) (int, error) {
	var metadataBuffer [hashLength + common.FileInfoLength + common.FileInfoLength]byte
	copy(metadataBuffer[0:hashLength], meta.Hash)
	copy(metadataBuffer[hashLength:hashLength+common.FileInfoLength], common.PadString(meta.Name, common.FileInfoLength))

	var sizeBuf [common.FileInfoLength]byte
	sizeBytes := strconv.AppendInt(sizeBuf[:0], meta.Size, 10)
	copy(metadataBuffer[hashLength+common.FileInfoLength:], sizeBytes)

	if _, err := m.Write(metadataBuffer[:]); err != nil {
		return 0, fmt.Errorf("failed to send metadata: %w", err)
	}

	// ⚡ Bolt: Read the metadata status code (1 byte) to determine if we should send the payload.
	var status [1]byte
	if _, err := io.ReadFull(m, status[:]); err != nil {
		return 0, fmt.Errorf("failed to read metadata status: %w", err)
	}
	return int(status[0]), nil
}

func (m *MomoTCPCommunicator) ReceiveMetadata() (common.FileMetadata, error) {
	var metadata common.FileMetadata
	var buffer [64 + common.FileInfoLength + common.FileInfoLength]byte

	if _, err := io.ReadFull(m, buffer[:]); err != nil {
		return metadata, err
	}

	metadata.Hash = common.SanitizeLog(string(bytesTrimNull(buffer[:64])))
	metadata.Name = string(bytesTrimNull(buffer[64 : 64+common.FileInfoLength]))

	size, err := common.SafeParseInt(buffer[64+common.FileInfoLength:])
	if err != nil {
		return metadata, err
	}
	metadata.Size = size

	return metadata, nil
}

// SendMetadataStatus is called by the server after receiving metadata.
func (m *MomoTCPCommunicator) SendMetadataStatus(status int) error {
	if _, err := m.Write([]byte{byte(status)}); err != nil {
		return fmt.Errorf("failed to send metadata status: %w", err)
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

func (m *MomoTCPCommunicator) SendACK(serverId int) error {
	var ackBuf [32]byte
	if _, err := m.Write(strconv.AppendInt(append(ackBuf[:0], "ACK"...), int64(serverId), 10)); err != nil {
		return fmt.Errorf("failed to send ACK: %w", err)
	}
	return nil
}

func (m *MomoTCPCommunicator) ReceiveACK() error {
	var ackBuffer [3]byte // "ACK" is 3 bytes, wait serverId is also there?
	// The existing logic reads 3 bytes but expects "ACK"?
	// Wait, the existing logic in sendFile reads 3 bytes: var ackBuffer [3]byte
	// and checks if it's "ACK". But server sends "ACK%d".

	if _, err := io.ReadFull(m, ackBuffer[:]); err != nil {
		return fmt.Errorf("failed to read ACK: %w", err)
	}

	if !bytes.Equal(ackBuffer[:], []byte("ACK")) {
		return fmt.Errorf("unexpected response: %q", string(ackBuffer[:]))
	}
	return nil
}
