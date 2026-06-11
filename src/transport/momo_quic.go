package transport

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"strconv"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/quic-go/quic-go"
)

// MomoQUICCommunicator implements the Communicator interface for the Momo protocol over QUIC.
type MomoQUICCommunicator struct {
	*quic.Stream
	conn *quic.Conn
}

// NewMomoQUICCommunicator creates a new MomoQUICCommunicator.
func NewMomoQUICCommunicator(stream *quic.Stream, conn *quic.Conn) *MomoQUICCommunicator {
	return &MomoQUICCommunicator{
		Stream: stream,
		conn:   conn,
	}
}

func (m *MomoQUICCommunicator) SetAbsoluteDeadline(t interface{}) error {
	deadline, ok := t.(time.Time)
	if !ok {
		return fmt.Errorf("invalid deadline type: expected time.Time")
	}
	m.Stream.SetDeadline(deadline)
	return nil
}

func (m *MomoQUICCommunicator) HandshakeClient(authToken string, timestamp int64, requestedMode int) (int, error) {
	var handshakeBuf [common.AuthTokenLength + common.TimestampLength + 1]byte
	copy(handshakeBuf[0:common.AuthTokenLength], common.PadString(authToken, common.AuthTokenLength))

	// Write the timestamp
	strconv.AppendInt(handshakeBuf[common.AuthTokenLength:common.AuthTokenLength], timestamp, 10)

	// Write the requested mode (1 byte) at the end
	handshakeBuf[common.AuthTokenLength+common.TimestampLength] = byte(strconv.Itoa(requestedMode)[0])

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

func (m *MomoQUICCommunicator) HandshakeServer(expectedAuthToken []byte) (int, int64, error) {
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

	requestedMode, err := common.SafeParseInt([]byte{requestedModeByte})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse requested mode: %w", err)
	}

	return int(requestedMode), timestamp, nil
}

func (m *MomoQUICCommunicator) SendReplicationMode(mode int) error {
	var repModeBuf [16]byte
	if _, err := m.Write(strconv.AppendInt(repModeBuf[:0], int64(mode), 10)); err != nil {
		return fmt.Errorf("failed to send replication mode: %w", err)
	}
	return nil
}

func (m *MomoQUICCommunicator) SendMetadata(meta *common.FileMetadata) (int, error) {
	var metadataBuffer [64 + common.FileInfoLength + common.FileInfoLength]byte
	copy(metadataBuffer[0:64], meta.Hash)
	copy(metadataBuffer[64:64+common.FileInfoLength], common.PadString(meta.Name, common.FileInfoLength))

	var sizeBuf [common.FileInfoLength]byte
	sizeBytes := strconv.AppendInt(sizeBuf[:0], meta.Size, 10)
	copy(metadataBuffer[64+common.FileInfoLength:], sizeBytes)

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

func (m *MomoQUICCommunicator) ReceiveMetadata() (common.FileMetadata, error) {
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
func (m *MomoQUICCommunicator) SendMetadataStatus(status int) error {
	if _, err := m.Write([]byte{byte(status)}); err != nil {
		return fmt.Errorf("failed to send metadata status: %w", err)
	}
	return nil
}

func (m *MomoQUICCommunicator) SendACK(serverId int) error {
	var ackBuf [32]byte
	if _, err := m.Write(strconv.AppendInt(append(ackBuf[:0], "ACK"...), int64(serverId), 10)); err != nil {
		return fmt.Errorf("failed to send ACK: %w", err)
	}
	return nil
}

func (m *MomoQUICCommunicator) ReceiveACK() error {
	var ackBuffer [3]byte
	if _, err := io.ReadFull(m, ackBuffer[:]); err != nil {
		return fmt.Errorf("failed to read ACK: %w", err)
	}

	if string(ackBuffer[:]) != "ACK" {
		return fmt.Errorf("unexpected response: %q", string(ackBuffer[:]))
	}
	return nil
}

func (m *MomoQUICCommunicator) RemoteAddr() net.Addr {
	return m.conn.RemoteAddr()
}

func (m *MomoQUICCommunicator) Close() error {
	return m.Stream.Close()
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
func DialQUIC(ctx context.Context, address string) (*quic.Conn, *quic.Stream, error) {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"momo-quic"},
	}
	conn, err := quic.DialAddr(ctx, address, tlsConf, nil)
	if err != nil {
		return nil, nil, err
	}
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		conn.CloseWithError(0, "failed to open stream")
		return nil, nil, err
	}
	return conn, stream, nil
}
