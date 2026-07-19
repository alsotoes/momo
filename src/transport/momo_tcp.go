package transport

import (
	"bytes"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

const hashLength = 64

// MomoTCPCommunicator implements the Communicator interface for the legacy Momo TCP protocol.
type MomoTCPCommunicator struct {
	*common.IdleTimeoutConn
	store storage.Store
}

// NewMomoTCPCommunicator creates a new MomoTCPCommunicator wrapping a net.Conn.
func NewMomoTCPCommunicator(conn net.Conn) *MomoTCPCommunicator {
	return &MomoTCPCommunicator{
		IdleTimeoutConn: common.NewIdleTimeoutConn(conn, 30*time.Second),
	}
}

func (m *MomoTCPCommunicator) SetStore(store storage.Store) {
	m.store = store
}

func (m *MomoTCPCommunicator) SetAbsoluteDeadline(t interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Recovered from panic in SetAbsoluteDeadline: %v", r)
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
			log.Printf("CRITICAL: Recovered from panic in HandshakeClient: %v", r)
			if m != nil {
				m.Close()
			}
			err = fmt.Errorf("panic in HandshakeClient: %v: %w", r, syscall.EIO)
		}
	}()

	var handshakeBuf [common.AuthTokenLength + common.TimestampLength + 1]byte
	copy(handshakeBuf[0:common.AuthTokenLength], common.PadString(authToken, common.AuthTokenLength))

	// ⚡ Bolt: Use PadString to ensure the timestamp is exactly 19 bytes and correctly placed.
	// We optimize this using a common helper to avoid intermediate string allocations.
	if err := common.AppendPaddedInt(handshakeBuf[common.AuthTokenLength:], timestamp, common.TimestampLength); err != nil {
		return 0, fmt.Errorf("failed to format handshake timestamp: %w", err)
	}
	
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
			log.Printf("CRITICAL: Recovered from panic in HandshakeServer: %v", r)
			if m != nil {
				m.Close()
			}
			err = fmt.Errorf("panic in HandshakeServer: %v: %w", r, syscall.EIO)
		}
	}()

	var handshakeBuf [common.AuthTokenLength + common.TimestampLength + 1]byte
	if _, err := io.ReadFull(io.LimitReader(m, common.AuthTokenLength+common.TimestampLength+1), handshakeBuf[:]); err != nil {
		return 0, 0, fmt.Errorf("failed to read handshake: %v: %w", err, syscall.EBADMSG)
	}

	// 🛡️ Zero-Crash: Verify handshake buffer length bounds before slicing (Rule 4)
	if len(handshakeBuf) < common.AuthTokenLength+common.TimestampLength+1 {
		return 0, 0, fmt.Errorf("handshake buffer too small: %w", syscall.EBADMSG)
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

	// Decode requestedModeByte polymorphically: characters 'L', 'D', 'G' represent file actions
	// while numeric bytes '0'-'9' represent replication strategies, separating namespaces.
	if requestedModeByte == 'L' || requestedModeByte == 'D' || requestedModeByte == 'G' {
		requestedMode = int(requestedModeByte)
	} else {
		requestedMode = int(requestedModeByte - '0')
		if requestedMode < 0 || requestedMode > 9 {
			return 0, 0, fmt.Errorf("invalid requested mode: %d: %w", requestedMode, syscall.EBADMSG)
		}
	}

	// 🛡️ Sentinel: Handle non-replication API queries (LIST, DELETE, GET) natively on Momo-TCP.
	if requestedMode == common.ModeList {
		if m.store == nil {
			return 0, 0, fmt.Errorf("storage store not initialized")
		}
		files, err := m.store.List()
		if err != nil {
			return 0, 0, fmt.Errorf("failed to list files: %w", err)
		}

		// Send file count (4 bytes big-endian)
		m.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := binary.Write(m, binary.BigEndian, int32(len(files))); err != nil {
			return 0, 0, fmt.Errorf("failed to send file count: %w", err)
		}

		// Send metadata packets (192 bytes each)
		for _, file := range files {
			// 🛡️ Sentinel: Validate length bounds
			if len(file.Name) > 64 || len(file.Hash) > 64 {
				continue
			}
			var packet [192]byte
			copy(packet[0:64], common.PadString(file.Hash, 64))
			wireName := file.Name
			if file.RemotePath != "" {
				wireName = file.RemotePath + "/" + file.Name
			}
			copy(packet[64:128], common.PadString(wireName, 64))
			if err := common.AppendPaddedInt(packet[128:], file.Size, 64); err != nil {
				return 0, 0, fmt.Errorf("failed to format file size: %v: %w", err, syscall.EINVAL)
			}

			if _, err := m.Write(packet[:]); err != nil {
				return 0, 0, fmt.Errorf("failed to write metadata packet: %v: %w", err, syscall.EIO)
			}
		}

		return 0, 0, ErrRequestHandled
	}

	if requestedMode == common.ModeDelete {
		if m.store == nil {
			return 0, 0, fmt.Errorf("storage store not initialized")
		}
		// Read 64-byte file name
		m.SetReadDeadline(time.Now().Add(5 * time.Second))
		var fileBuf [64]byte
		if _, err := io.ReadFull(m, fileBuf[:]); err != nil {
			return 0, 0, fmt.Errorf("failed to read delete target: %w", err)
		}
		fileName := common.TrimNullBytesString(fileBuf[:])

		// 🛡️ Sentinel: Block path traversal
		if strings.Contains(fileName, "..") || strings.Contains(fileName, "\\") {
			m.SetWriteDeadline(time.Now().Add(5 * time.Second))
			m.Write([]byte{'1'}) // error status
			return 0, 0, fmt.Errorf("invalid delete target traversal: %s: %w", fileName, syscall.EBADMSG)
		}

		err = m.store.Delete(fileName)
		m.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err != nil {
			m.Write([]byte{'1'}) // error status
			return 0, 0, fmt.Errorf("failed to delete file: %w", err)
		}

		m.Write([]byte{'0'}) // success status
		return 0, 0, ErrRequestHandled
	}

	if requestedMode == common.ModeGet {
		if m.store == nil {
			return 0, 0, fmt.Errorf("storage store not initialized")
		}
		// Read 64-byte file name
		m.SetReadDeadline(time.Now().Add(5 * time.Second))
		var fileBuf [64]byte
		if _, err := io.ReadFull(m, fileBuf[:]); err != nil {
			return 0, 0, fmt.Errorf("failed to read get target: %w", err)
		}
		fileName := common.TrimNullBytesString(fileBuf[:])

		// 🛡️ Sentinel: Block path traversal
		if strings.Contains(fileName, "..") || strings.Contains(fileName, "\\") {
			m.SetWriteDeadline(time.Now().Add(5 * time.Second))
			m.Write([]byte{'1'}) // error status
			return 0, 0, fmt.Errorf("invalid get target traversal: %s: %w", fileName, syscall.EBADMSG)
		}

		rc, meta, err := m.store.Get(fileName)
		m.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err != nil {
			if err == syscall.ENOENT || os.IsNotExist(err) {
				m.Write([]byte{'1'}) // file not found
				return 0, 0, ErrRequestHandled
			}
			m.Write([]byte{'2'}) // server error
			return 0, 0, fmt.Errorf("failed to read file: %w", err)
		}
		defer rc.Close()

		// Write '0' (success status) + 64-byte size string
		var respBuf [65]byte
		respBuf[0] = '0'
		if err := common.AppendPaddedInt(respBuf[1:], meta.Size, 64); err != nil {
			return 0, 0, fmt.Errorf("failed to format GET file size: %v: %w", err, syscall.EINVAL)
		}
		if _, err := m.Write(respBuf[:]); err != nil {
			return 0, 0, fmt.Errorf("failed to send get ACK: %v: %w", err, syscall.EIO)
		}

		// Progressive write deadline for payload copying (5s floor + 1s per MB)
		copyTimeout := 5 * time.Second
		mb := meta.Size / (1024 * 1024)
		if mb > 0 {
			copyTimeout += time.Duration(mb) * time.Second
		}
		m.SetWriteDeadline(time.Now().Add(copyTimeout))

		if _, err := io.Copy(m, rc); err != nil {
			return 0, 0, fmt.Errorf("failed to stream file payload: %w", err)
		}

		return 0, 0, ErrRequestHandled
	}

	return requestedMode, timestamp, nil
}

// SendReplicationMode is a helper for HandshakeServer to send the selected mode back.
func (m *MomoTCPCommunicator) SendReplicationMode(mode int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Recovered from panic in SendReplicationMode: %v", r)
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
			log.Printf("CRITICAL: Recovered from panic in SendMetadata: %v", r)
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
			log.Printf("CRITICAL: Recovered from panic in ReceiveMetadata: %v", r)
			err = fmt.Errorf("panic in ReceiveMetadata: %v: %w", r, syscall.EIO)
		}
	}()

	var metadata common.FileMetadata
	var buffer [hashLength + common.FileInfoLength + common.FileInfoLength]byte

	if _, err := io.ReadFull(io.LimitReader(m, hashLength+common.FileInfoLength+common.FileInfoLength), buffer[:]); err != nil {
		return metadata, err
	}

	// 🛡️ Zero-Crash: Verify metadata buffer length bounds before slicing (Rule 4)
	if len(buffer) < hashLength+common.FileInfoLength+common.FileInfoLength {
		return metadata, fmt.Errorf("metadata buffer too small: %w", syscall.EBADMSG)
	}

	metadata.Hash = common.SanitizeLog(string(bytesTrimNull(buffer[:hashLength])))
	// 🛡️ Sentinel: Sanitize hash immediately to prevent path traversal in all downstream consumers.
	if metadata.Hash == "" || common.HasPathTraversalChars(metadata.Hash) {
		return common.FileMetadata{}, fmt.Errorf("invalid hash: %s: %w", metadata.Hash, syscall.EBADMSG)
	}
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
			log.Printf("CRITICAL: Recovered from panic in SendMetadataStatus: %v", r)
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
			log.Printf("CRITICAL: Recovered from panic in SendACK: %v", r)
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
			log.Printf("CRITICAL: Recovered from panic in ReceiveACK: %v", r)
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
