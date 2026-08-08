package transport

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"github.com/quic-go/quic-go"
)

// MomoQUICCommunicator implements the Communicator interface for the Momo protocol over QUIC.
type MomoQUICCommunicator struct {
	*quic.Stream
	conn             *quic.Conn
	store            storage.Store
	globalLister     GlobalLister
	leaseAcquirer    LeaseAcquirer
	deletePropagator DeletePropagator
	metricsHook      MetricsHook
	oprfService      OPRFService
	isPeer           bool
	useChallengeResp bool
}

// NewMomoQUICCommunicator creates a new MomoQUICCommunicator.
func NewMomoQUICCommunicator(stream *quic.Stream, conn *quic.Conn) *MomoQUICCommunicator {
	return &MomoQUICCommunicator{
		Stream: stream,
		conn:   conn,
	}
}

// SetChallengeResponse enables or disables challenge-response authentication.
func (m *MomoQUICCommunicator) SetChallengeResponse(enabled bool) {
	m.useChallengeResp = enabled
}

func (m *MomoQUICCommunicator) SetStore(store storage.Store) {
	m.store = store
}

// SetGlobalLister sets the scatter-gather list capability.
func (m *MomoQUICCommunicator) SetGlobalLister(gl GlobalLister) {
	m.globalLister = gl
}

// SetLeaseAcquirer sets the lease-based consensus capability.
func (m *MomoQUICCommunicator) SetLeaseAcquirer(la LeaseAcquirer) {
	m.leaseAcquirer = la
}

// SetDeletePropagator sets the P2P delete propagation capability.
func (m *MomoQUICCommunicator) SetDeletePropagator(dp DeletePropagator) {
	m.deletePropagator = dp
}

// SetMetricsHook sets the metrics instrumentation hook.
func (m *MomoQUICCommunicator) SetMetricsHook(hook MetricsHook) {
	m.metricsHook = hook
}

// SetOPRFService sets the threshold-OPRF evaluation service used to answer
// ModeOPRFEval requests from clients.
func (m *MomoQUICCommunicator) SetOPRFService(s OPRFService) {
	m.oprfService = s
}

// SendOPRFEval performs a threshold-OPRF evaluation request over Momo-QUIC.
// It sends a handshake with mode 'O', the blinded dedup tag, then reads the
// share evaluations returned by the daemon quorum. It never reveals the
// unblinded tag to the server and fails closed when fewer than threshold
// distinct evaluations are returned.
func (m *MomoQUICCommunicator) SendOPRFEval(authToken string, timestamp int64, blinded []byte, threshold int) (results []OPRFEvalResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Recovered from panic in SendOPRFEval: %v", r)
			if m != nil {
				m.Close()
			}
			err = fmt.Errorf("panic in SendOPRFEval: %v: %w", r, syscall.EIO)
		}
	}()

	if len(blinded) != 32 {
		return nil, fmt.Errorf("oprf: blinded tag must be 32 bytes: %w", syscall.EINVAL)
	}

	// Manual handshake with mode 'O' (like GET/Download sends literal 'G').
	// Respect challenge-response authentication when enabled.
	if m.useChallengeResp {
		var handshakeBuf [common.TimestampLength + 1]byte
		if err := common.WritePaddedInt(handshakeBuf[:common.TimestampLength], timestamp, common.TimestampLength); err != nil {
			return nil, fmt.Errorf("oprf: failed to format handshake timestamp: %w", err)
		}
		handshakeBuf[common.TimestampLength] = byte(common.ModeOPRFEval)
		if _, err := m.Write(handshakeBuf[:]); err != nil {
			return nil, fmt.Errorf("oprf: failed to send handshake: %v: %w", err, syscall.EIO)
		}
		if err := common.ChallengeResponseClient(m, authToken); err != nil {
			return nil, fmt.Errorf("oprf: challenge-response auth failed: %w", err)
		}
	} else {
		var handshakeBuf [common.AuthTokenLength + common.TimestampLength + 1]byte
		copy(handshakeBuf[:common.AuthTokenLength], common.PadString(authToken, common.AuthTokenLength))
		if err := common.WritePaddedInt(handshakeBuf[common.AuthTokenLength:], timestamp, common.TimestampLength); err != nil {
			return nil, fmt.Errorf("oprf: failed to format handshake timestamp: %w", err)
		}
		handshakeBuf[common.AuthTokenLength+common.TimestampLength] = byte(common.ModeOPRFEval)
		if _, err := m.Write(handshakeBuf[:]); err != nil {
			return nil, fmt.Errorf("oprf: failed to send handshake: %v: %w", err, syscall.EIO)
		}
	}

	if _, err := m.Write(blinded); err != nil {
		return nil, fmt.Errorf("oprf: failed to send blinded tag: %v: %w", err, syscall.EIO)
	}

	var countBuf [4]byte
	if _, err := io.ReadFull(m, countBuf[:]); err != nil {
		return nil, fmt.Errorf("oprf: failed to read result count: %v: %w", err, syscall.EBADMSG)
	}
	count := int(binary.BigEndian.Uint32(countBuf[:]))
	if count == 0 {
		return nil, fmt.Errorf("oprf: no evaluations returned (quorum not met): %w", syscall.EAGAIN)
	}
	if count > 255 {
		return nil, fmt.Errorf("oprf: implausible evaluation count %d: %w", count, syscall.EBADMSG)
	}

	results = make([]OPRFEvalResult, 0, count)
	for i := 0; i < count; i++ {
		var idxBuf [4]byte
		if _, err := io.ReadFull(m, idxBuf[:]); err != nil {
			return nil, fmt.Errorf("oprf: failed to read share index: %v: %w", err, syscall.EBADMSG)
		}
		var lenBuf [4]byte
		if _, err := io.ReadFull(m, lenBuf[:]); err != nil {
			return nil, fmt.Errorf("oprf: failed to read eval length: %v: %w", err, syscall.EBADMSG)
		}
		evalLen := int(binary.BigEndian.Uint32(lenBuf[:]))
		if evalLen > 32 {
			return nil, fmt.Errorf("oprf: implausible eval length %d: %w", evalLen, syscall.EBADMSG)
		}
		eval := make([]byte, evalLen)
		if _, err := io.ReadFull(m, eval); err != nil {
			return nil, fmt.Errorf("oprf: failed to read eval: %v: %w", err, syscall.EBADMSG)
		}
		results = append(results, OPRFEvalResult{
			ShareIndex: int(binary.BigEndian.Uint32(idxBuf[:])),
			Eval:       eval,
		})
	}

	if len(results) < threshold {
		return nil, fmt.Errorf("oprf: only %d evaluations, need %d: %w", len(results), threshold, syscall.EAGAIN)
	}
	return results, nil
}

func (m *MomoQUICCommunicator) SetAbsoluteDeadline(t interface{}) (err error) {
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
	m.Stream.SetDeadline(deadline)
	return nil
}

func (m *MomoQUICCommunicator) HandshakeClient(authToken string, timestamp int64, requestedMode int) (mode int, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Recovered from panic in HandshakeClient: %v", r)
			if m != nil {
				m.Close()
			}
			err = fmt.Errorf("panic in HandshakeClient: %v: %w", r, syscall.EIO)
		}
	}()

	if m.useChallengeResp {
		var handshakeBuf [common.TimestampLength + 1]byte
		if err := common.WritePaddedInt(handshakeBuf[:common.TimestampLength], timestamp, common.TimestampLength); err != nil {
			return 0, fmt.Errorf("failed to format handshake timestamp: %w", err)
		}
		handshakeBuf[common.TimestampLength] = byte(requestedMode + '0')

		if _, err := m.Write(handshakeBuf[:]); err != nil {
			return 0, fmt.Errorf("failed to send handshake: %v: %w", err, syscall.EIO)
		}

		if err := common.ChallengeResponseClient(m, authToken); err != nil {
			return 0, err
		}
	} else {
		var handshakeBuf [common.AuthTokenLength + common.TimestampLength + 1]byte
		copy(handshakeBuf[0:common.AuthTokenLength], common.PadString(authToken, common.AuthTokenLength))

		if err := common.WritePaddedInt(handshakeBuf[common.AuthTokenLength:], timestamp, common.TimestampLength); err != nil {
			return 0, fmt.Errorf("failed to format handshake timestamp: %w", err)
		}

		handshakeBuf[common.AuthTokenLength+common.TimestampLength] = byte(requestedMode + '0')

		if _, err := m.Write(handshakeBuf[:]); err != nil {
			return 0, fmt.Errorf("failed to send handshake: %v: %w", err, syscall.EIO)
		}
	}

	var respBuf [1]byte
	if _, err := io.ReadFull(io.LimitReader(m, 1), respBuf[:]); err != nil {
		return 0, fmt.Errorf("failed to read replication mode response: %v: %w", err, syscall.EBADMSG)
	}

	replicationModeInt64, err := common.SafeParseInt(respBuf[:])
	if err != nil {
		return 0, fmt.Errorf("invalid replication mode response: %v: %w", err, syscall.EBADMSG)
	}

	return int(replicationModeInt64), nil
}

func (m *MomoQUICCommunicator) HandshakeServer(expectedAuthToken []byte) (requestedMode int, timestamp int64, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Recovered from panic in HandshakeServer: %v", r)
			if m != nil {
				m.Close()
			}
			err = fmt.Errorf("panic in HandshakeServer: %v: %w", r, syscall.EIO)
		}
	}()

	if m.useChallengeResp {
		var handshakeBuf [common.TimestampLength + 1]byte
		if _, err := io.ReadFull(io.LimitReader(m, common.TimestampLength+1), handshakeBuf[:]); err != nil {
			return 0, 0, fmt.Errorf("failed to read handshake: %v: %w", err, syscall.EBADMSG)
		}

		bufferTimestamp := handshakeBuf[:common.TimestampLength]
		requestedModeByte := handshakeBuf[common.TimestampLength]

		isPeer, authErr := common.ChallengeResponseServerPeer(m, expectedAuthToken)
		if authErr != nil {
			return 0, 0, authErr
		}
		m.isPeer = isPeer

		timestamp, err = common.SafeParseInt(bufferTimestamp)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to parse timestamp: %v: %w", err, syscall.EBADMSG)
		}

		if requestedModeByte == 'L' || requestedModeByte == 'D' || requestedModeByte == 'G' || requestedModeByte == 'O' {
			requestedMode = int(requestedModeByte)
		} else {
			requestedMode = int(requestedModeByte - '0')
			if requestedMode < 0 || requestedMode > 9 {
				return 0, 0, fmt.Errorf("invalid requested mode: %d: %w", requestedMode, syscall.EBADMSG)
			}
		}
	} else {
		var handshakeBuf [common.AuthTokenLength + common.TimestampLength + 1]byte
		if _, err := io.ReadFull(io.LimitReader(m, common.AuthTokenLength+common.TimestampLength+1), handshakeBuf[:]); err != nil {
			return 0, 0, fmt.Errorf("failed to read handshake: %v: %w", err, syscall.EBADMSG)
		}

		if len(handshakeBuf) < common.AuthTokenLength+common.TimestampLength+1 {
			return 0, 0, fmt.Errorf("handshake buffer too small: %w", syscall.EBADMSG)
		}

		bufferAuthToken := handshakeBuf[:common.AuthTokenLength]
		bufferTimestamp := handshakeBuf[common.AuthTokenLength : common.AuthTokenLength+common.TimestampLength]
		requestedModeByte := handshakeBuf[common.AuthTokenLength+common.TimestampLength]

		if subtle.ConstantTimeCompare(bufferAuthToken, expectedAuthToken) == 1 {
			m.isPeer = false
		} else if peerToken := common.DerivePeerToken(expectedAuthToken); subtle.ConstantTimeCompare(bufferAuthToken, peerToken) == 1 {
			m.isPeer = true
		} else {
			return 0, 0, syscall.EACCES
		}

		timestamp, err = common.SafeParseInt(bufferTimestamp)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to parse timestamp: %v: %w", err, syscall.EBADMSG)
		}

		if requestedModeByte == 'L' || requestedModeByte == 'D' || requestedModeByte == 'G' || requestedModeByte == 'O' {
			requestedMode = int(requestedModeByte)
		} else {
			requestedMode = int(requestedModeByte - '0')
			if requestedMode < 0 || requestedMode > 9 {
				return 0, 0, fmt.Errorf("invalid requested mode: %d: %w", requestedMode, syscall.EBADMSG)
			}
		}
	}

	// 🛡️ Sentinel: Handle non-replication API queries (LIST, DELETE, GET) natively on Momo-QUIC.
	if requestedMode == common.ModeList {
		// 🛡️ CVE-001: Restrict LIST to peer connections only.
		// Direct clients cannot enumerate all files. Peers need LIST for
		// replication (scatter-gather). Clients should track their own files.
		if !m.isPeer {
			m.Stream.SetWriteDeadline(time.Now().Add(5 * time.Second))
			var emptyCount [4]byte
			binary.BigEndian.PutUint32(emptyCount[:], 0)
			m.Write(emptyCount[:])
			return 0, 0, ErrRequestHandled
		}
		if m.store == nil {
			return 0, 0, fmt.Errorf("storage store not initialized")
		}
		var files []common.FileMetadata
		if m.globalLister != nil {
			files, err = m.globalLister.GlobalList(5 * time.Second)
		} else {
			files, err = m.store.List()
		}
		if err != nil {
			return 0, 0, fmt.Errorf("failed to list files: %w", err)
		}

		// Send file count (4 bytes big-endian)
		m.Stream.SetWriteDeadline(time.Now().Add(5 * time.Second))
		// ⚡ Bolt: Eliminate binary.Write reflection and allocations.
		var countBuf [4]byte
		binary.BigEndian.PutUint32(countBuf[:], uint32(len(files)))
		if _, err := m.Write(countBuf[:]); err != nil {
			return 0, 0, fmt.Errorf("failed to send file count: %w", err)
		}

		// Send metadata packets (192 bytes each)
		for _, file := range files {
			// 🛡️ Sentinel: Validate length bounds
			if len(file.Hash) > 64 {
				continue
			}
			wireName := file.Name
			if file.RemotePath != "" {
				wireName = file.RemotePath + "/" + file.Name
			}
			if len(wireName) > 64 {
				continue
			}
			var packet [192]byte
			copy(packet[0:64], common.PadString(file.Hash, 64))
			copy(packet[64:128], common.PadString(wireName, 64))
			if err := common.WritePaddedInt(packet[128:], file.Size, 64); err != nil {
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
		// Read 64-byte file name + 64-byte content hash (proof of knowledge)
		m.Stream.SetReadDeadline(time.Now().Add(5 * time.Second))
		var requestBuf [128]byte
		if _, err := io.ReadFull(m, requestBuf[:]); err != nil {
			return 0, 0, fmt.Errorf("failed to read delete target: %w", err)
		}
		fileName := common.TrimNullBytesString(requestBuf[:64])
		providedHash := common.TrimNullBytesString(requestBuf[64:128])

		// 🛡️ Sentinel: Block path traversal
		if strings.Contains(fileName, "..") || strings.Contains(fileName, "\\") {
			m.Stream.SetWriteDeadline(time.Now().Add(5 * time.Second))
			m.Write([]byte{'1'}) // error status
			return 0, 0, fmt.Errorf("invalid delete target traversal: %s: %w", fileName, syscall.EBADMSG)
		}

		// 🛡️ CVE-003: Require proof-of-knowledge (content hash) for DELETE.
		// The client must provide the file's content hash, which is verified
		// against the namespace mapping. This prevents deleting files by
		// name alone without knowing the content hash.
		expectedHash, hashErr := m.store.GetHashForName(fileName)
		if hashErr != nil || expectedHash != providedHash {
			m.Stream.SetWriteDeadline(time.Now().Add(5 * time.Second))
			m.Write([]byte{'1'}) // not found / unauthorized
			return 0, 0, ErrRequestHandled
		}

		if m.leaseAcquirer != nil {
			if err := m.leaseAcquirer.AcquireLease(fileName, 10*time.Second); err != nil {
				m.Stream.SetWriteDeadline(time.Now().Add(5 * time.Second))
				m.Write([]byte{'1'}) // error status
				return 0, 0, fmt.Errorf("failed to acquire lease for delete: %w", err)
			}
			defer m.leaseAcquirer.ReleaseLease(fileName)
		}

		err = m.store.Delete(fileName)
		m.Stream.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err != nil {
			m.Write([]byte{'1'}) // error status
			return 0, 0, fmt.Errorf("failed to delete file: %w", err)
		}

		if m.deletePropagator != nil {
			_ = m.deletePropagator.PropagateDelete(fileName, 5*time.Second)
		}

		if m.metricsHook != nil {
			m.metricsHook.IncDeletes()
		}

		m.Write([]byte{'0'}) // success status
		return 0, 0, ErrRequestHandled
	}

	if requestedMode == common.ModeGet {
		if m.store == nil {
			return 0, 0, fmt.Errorf("storage store not initialized")
		}
		// Read 64-byte file name + 64-byte content hash (proof of knowledge)
		m.Stream.SetReadDeadline(time.Now().Add(5 * time.Second))
		var requestBuf [128]byte
		if _, err := io.ReadFull(m, requestBuf[:]); err != nil {
			return 0, 0, fmt.Errorf("failed to read get target: %w", err)
		}
		fileName := common.TrimNullBytesString(requestBuf[:64])
		providedHash := common.TrimNullBytesString(requestBuf[64:128])

		// 🛡️ Sentinel: Block path traversal
		if strings.Contains(fileName, "..") || strings.Contains(fileName, "\\") {
			m.Stream.SetWriteDeadline(time.Now().Add(5 * time.Second))
			m.Write([]byte{'1'}) // error status
			return 0, 0, fmt.Errorf("invalid get target traversal: %s: %w", fileName, syscall.EBADMSG)
		}

		// 🛡️ CVE-009: Require proof-of-knowledge (content hash) for GET.
		expectedHash, hashErr := m.store.GetHashForName(fileName)
		if hashErr != nil || expectedHash != providedHash {
			m.Stream.SetWriteDeadline(time.Now().Add(5 * time.Second))
			m.Write([]byte{'1'}) // not found / unauthorized
			return 0, 0, ErrRequestHandled
		}

		rc, meta, err := m.store.Get(fileName)
		m.Stream.SetWriteDeadline(time.Now().Add(5 * time.Second))
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
		if err := common.WritePaddedInt(respBuf[1:], meta.Size, 64); err != nil {
			return 0, 0, fmt.Errorf("failed to format GET file size: %v: %w", err, syscall.EINVAL)
		}
		if _, err := m.Write(respBuf[:]); err != nil {
			return 0, 0, fmt.Errorf("failed to send get ACK: %v: %w", err, syscall.EIO)
		}

		// Progressive write deadline for payload copying (5s floor + 1s per MB)
		copyTimeout := 5 * time.Second
		mb := meta.Size / (1024 * 1024)
		if mb > 0 {
			maxMB := int64(math.MaxInt64 / int64(time.Second))
			if mb > maxMB {
				mb = maxMB
			}
			copyTimeout += time.Duration(mb) * time.Second
		}
		m.Stream.SetWriteDeadline(time.Now().Add(copyTimeout))

		if _, err := io.Copy(m, rc); err != nil {
			return 0, 0, fmt.Errorf("failed to stream file payload: %w", err)
		}

		if m.metricsHook != nil {
			m.metricsHook.IncDownloads()
			m.metricsHook.AddBytesDownloaded(uint64(meta.Size))
		}

		return 0, 0, ErrRequestHandled
	}

	// 🛡️ Sentinel: Handle threshold-OPRF evaluation requests natively on Momo-QUIC.
	if requestedMode == common.ModeOPRFEval {
		if m.oprfService == nil {
			m.Stream.SetWriteDeadline(time.Now().Add(5 * time.Second))
			var zeroCount [4]byte
			m.Write(zeroCount[:])
			return 0, 0, fmt.Errorf("oprf service not configured: %w", syscall.ENOTSUP)
		}

		m.Stream.SetReadDeadline(time.Now().Add(10 * time.Second))
		var blinded [32]byte
		if _, err := io.ReadFull(m, blinded[:]); err != nil {
			return 0, 0, fmt.Errorf("failed to read blinded tag: %v: %w", err, syscall.EBADMSG)
		}

		results, err := m.oprfService.EvaluateOPRF(blinded[:], 10*time.Second)
		if err != nil {
			m.Stream.SetWriteDeadline(time.Now().Add(5 * time.Second))
			var zeroCount [4]byte
			m.Write(zeroCount[:])
			return 0, 0, fmt.Errorf("oprf evaluation failed: %w", err)
		}

		m.Stream.SetWriteDeadline(time.Now().Add(5 * time.Second))
		var countBuf [4]byte
		binary.BigEndian.PutUint32(countBuf[:], uint32(len(results)))
		if _, err := m.Write(countBuf[:]); err != nil {
			return 0, 0, fmt.Errorf("failed to send oprf result count: %v: %w", err, syscall.EIO)
		}
		for _, r := range results {
			var idxBuf [4]byte
			binary.BigEndian.PutUint32(idxBuf[:], uint32(r.ShareIndex))
			if _, err := m.Write(idxBuf[:]); err != nil {
				return 0, 0, fmt.Errorf("failed to send oprf share index: %v: %w", err, syscall.EIO)
			}
			var lenBuf [4]byte
			binary.BigEndian.PutUint32(lenBuf[:], uint32(len(r.Eval)))
			if _, err := m.Write(lenBuf[:]); err != nil {
				return 0, 0, fmt.Errorf("failed to send oprf eval length: %v: %w", err, syscall.EIO)
			}
			if _, err := m.Write(r.Eval); err != nil {
				return 0, 0, fmt.Errorf("failed to send oprf eval: %v: %w", err, syscall.EIO)
			}
		}

		return 0, 0, ErrRequestHandled
	}

	return requestedMode, timestamp, nil
}

func (m *MomoQUICCommunicator) SendReplicationMode(mode int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Recovered from panic in SendReplicationMode: %v", r)
			err = fmt.Errorf("panic in SendReplicationMode: %v: %w", r, syscall.EIO)
		}
	}()

	var repModeBuf [16]byte
	if mode < 0 || mode > 9 {
		return fmt.Errorf("invalid replication mode %d: %w", mode, syscall.EBADMSG)
	}
	repModeBuf[0] = byte(mode + '0')
	if _, err := m.Write(repModeBuf[:1]); err != nil {
		return fmt.Errorf("failed to send replication mode: %v: %w", err, syscall.EIO)
	}
	return nil
}

func (m *MomoQUICCommunicator) SendMetadata(meta *common.FileMetadata) (status int, err error) {
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
	for _, part := range strings.Split(wireName, "/") {
		if common.HasPathTraversalChars(part) {
			return 0, fmt.Errorf("path traversal in wireName: %w", syscall.EBADMSG)
		}
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

func (m *MomoQUICCommunicator) ReceiveMetadata() (meta common.FileMetadata, err error) {
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

	// ⚡ Bolt: Use common.TrimNullBytesString to eliminate string allocation overhead
	metadata.Hash = common.SanitizeLog(common.TrimNullBytesString(buffer[:hashLength]))
	// 🛡️ Sentinel: Sanitize hash immediately to prevent path traversal in all downstream consumers.
	if common.HasPathTraversalChars(metadata.Hash) {
		return common.FileMetadata{}, fmt.Errorf("invalid hash: %s: %w", metadata.Hash, syscall.EBADMSG)
	}
	// ⚡ Bolt: Use common.TrimNullBytesString to eliminate string allocation overhead
	metadata.Name = common.TrimNullBytesString(buffer[hashLength : hashLength+common.FileInfoLength])

	size, err := common.SafeParseInt(buffer[hashLength+common.FileInfoLength:])
	if err != nil {
		return metadata, err
	}
	metadata.Size = size

	return metadata, nil
}

// SendMetadataStatus is called by the server after receiving metadata.
func (m *MomoQUICCommunicator) SendMetadataStatus(status int) (err error) {
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

func (m *MomoQUICCommunicator) SendACK(serverId int) (err error) {
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

func (m *MomoQUICCommunicator) ReceiveACK() (err error) {
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
	m.Stream.SetDeadline(time.Now().Add(5 * time.Millisecond))
	var oneByte [1]byte
	for i := 0; i < 10; i++ {
		n, _ := m.Read(oneByte[:])
		if n == 1 && oneByte[0] >= '0' && oneByte[0] <= '9' {
			// Continue
		} else {
			break
		}
	}
	m.Stream.SetDeadline(time.Time{}) // Restore default deadline
	return nil
}

func (m *MomoQUICCommunicator) RemoteAddr() net.Addr {
	return m.conn.RemoteAddr()
}

func (m *MomoQUICCommunicator) IsExternalClient() bool {
	return false
}

func (m *MomoQUICCommunicator) IsPeer() bool {
	return m.isPeer
}

func (m *MomoQUICCommunicator) Close() (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Recovered from panic in Close: %v", r)
			err = fmt.Errorf("panic in Close: %v: %w", r, syscall.EIO)
		}
	}()
	streamErr := m.Stream.Close()
	go func() {
		time.Sleep(100 * time.Millisecond)
		m.conn.CloseWithError(0, "")
	}()
	return streamErr
}

// GenerateSelfSignedCert generates a self-signed TLS certificate for testing and internal use.
func GenerateSelfSignedCert() (tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Momo"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(time.Hour * 24 * 365),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  key,
	}, nil
}

// DialQUIC connects to a peer using QUIC.
// When caCertPool is non-nil, peer certificates are verified against it.
// When caCertPool is nil and tlsInsecure is false, the connection fails.
// When caCertPool is nil and tlsInsecure is true, InsecureSkipVerify is used with a warning.
func DialQUIC(ctx context.Context, address string, caCertPool *x509.CertPool, tlsInsecure bool) (conn *quic.Conn, stream *quic.Stream, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in DialQUIC for %s: %v", address, r)
			if conn != nil {
				conn.CloseWithError(0, "panic recovery")
			}
			conn = nil
			stream = nil
			err = fmt.Errorf("panic in DialQUIC: %v: %w", r, syscall.ECONNREFUSED)
		}
	}()

	tlsConf := &tls.Config{
		NextProtos: []string{"momo-quic"},
	}
	if caCertPool != nil {
		tlsConf.RootCAs = caCertPool
	} else if tlsInsecure {
		log.Printf("WARNING: QUIC dial to %s using InsecureSkipVerify (tls_insecure=true) — vulnerable to MITM", address)
		tlsConf.InsecureSkipVerify = true
	} else {
		return nil, nil, fmt.Errorf("QUIC peer verification requires ca_cert or tls_insecure=true: %w", syscall.EACCES)
	}
	conn, err = quic.DialAddr(ctx, address, tlsConf, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("quic dial failed: %v: %w", err, syscall.ECONNREFUSED)
	}
	stream, err = conn.OpenStreamSync(ctx)
	if err != nil {
		conn.CloseWithError(0, "failed to open stream")
		return nil, nil, fmt.Errorf("quic stream open failed: %v: %w", err, syscall.ECONNREFUSED)
	}
	return conn, stream, nil
}
