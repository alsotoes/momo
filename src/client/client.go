package client

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"syscall"

	"github.com/alsotoes/momo/src/common"
	momocrypto "github.com/alsotoes/momo/src/crypto"
	"github.com/alsotoes/momo/src/transport"
)

// Connect establishes connections with daemon(s) and sends a file.
// It first connects to a specified daemon to determine the replication mode.
// If splay replication is active, it connects to all other daemons.
// Finally, it sends the file to all established connections concurrently.
func Connect(wg *sync.WaitGroup, cfg common.Configuration, filePath string, remotePath string, serverId int, timestamp int64, requestedMode int, replicationFactor int) (err error) {
	var communicators []transport.Communicator
	defer wg.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in client.Connect: %v", r)
			for _, c := range communicators {
				c.Close()
			}
			err = fmt.Errorf("panic in Connect: %v: %w", r, syscall.EIO)
		}
	}()
	daemons := cfg.Daemons
	if serverId < 0 || serverId >= len(daemons) {
		log.Printf("Server ID %d is out of range", serverId)
		return
	}
	authToken := cfg.Global.AuthToken
	factory := transport.NewProtocolFactory(cfg)
	var wgSendFile sync.WaitGroup

	// Load encryption cipher if E2EE is enabled.
	var encCipher *momocrypto.Cipher
	var tenantKey []byte
	var encryptedContent []byte
	if cfg.Global.EncryptionEnabled {
		encCipher, err = momocrypto.NewCipherFromHex(cfg.Global.EncryptionKey)
		if err != nil {
			log.Printf("Failed to create encryption cipher: %v", err)
			return
		}
		masterKey, decErr := hex.DecodeString(cfg.Global.EncryptionKey)
		if decErr != nil {
			log.Printf("Failed to decode encryption key: %v", decErr)
			return
		}
		tenantKey, err = momocrypto.DeriveKey(masterKey, cfg.Global.EncryptionTenant, nil)
		if err != nil {
			log.Printf("Failed to derive tenant key: %v", err)
			return
		}
	}

	// Connect to the initial daemon to check replication mode
	comm, err := factory.Dial(daemons[serverId].Host)
	if err != nil {
		log.Printf("Failed to connect to initial daemon %s: %v", daemons[serverId].Host, common.SanitizeLog(err.Error()))
		return
	}
	communicators = append(communicators, comm)

	// Perform handshake to get replication mode
	replicationMode, err := comm.HandshakeClient(authToken, timestamp, requestedMode)
	if err != nil {
		log.Printf("Handshake failed with %s: %v", daemons[serverId].Host, common.SanitizeLog(err.Error()))
		comm.Close()
		return
	}

	// Compute file hash and optionally encrypt content.
	// ⚡ Bolt: Use streaming encryption to avoid loading the entire plaintext
	// into memory. EncryptStream reads in 4KB chunks and writes to a buffer,
	// so peak memory is chunk-sized rather than file-sized.
	var fileHash string
	if encCipher != nil {
		// 🛡️ Zero-Crash: Validate file size before encryption to prevent
		// unbounded buffer growth (Rule 4).
		fileInfo, rErr := os.Stat(filePath)
		if rErr != nil {
			log.Printf("Failed to stat file %s: %v", common.SanitizeLog(filePath), common.SanitizeLog(rErr.Error()))
			return
		}
		if fileInfo.Size() > common.MaxFileSize {
			log.Printf("File %s size %d exceeds maximum %d: %v", common.SanitizeLog(filePath), fileInfo.Size(), common.MaxFileSize, syscall.EFBIG)
			return
		}
		file, rErr := os.Open(filePath)
		if rErr != nil {
			log.Printf("Failed to open file %s: %v", common.SanitizeLog(filePath), common.SanitizeLog(rErr.Error()))
			return
		}
		var encBuf bytes.Buffer
		if err = encCipher.EncryptStream(file, &encBuf); err != nil {
			file.Close()
			log.Printf("Failed to encrypt file %s: %v", common.SanitizeLog(filePath), err)
			return
		}
		file.Close()
		encryptedContent = encBuf.Bytes()
		h := sha256.New()
		h.Write(encryptedContent)
		fileHash = hex.EncodeToString(h.Sum(nil))
	} else {
		fileHash, err = common.HashFile(filePath)
		if err != nil {
			log.Printf("Failed to hash file %s: %v", common.SanitizeLog(filePath), common.SanitizeLog(err.Error()))
			return
		}
	}

	if replicationMode == common.ReplicationPrimarySplay {
		// ⚡ Bolt: Use CRUSH to find the specific replicas for PrimarySplay.
		nodes := make([]*common.Node, len(daemons))
		for i, d := range daemons {
			nodes[i] = &common.Node{ID: i, Weight: 1, Addr: d.Host}
		}
		cmap := &common.ClusterMap{Nodes: nodes}
		placement, err := cmap.Placement(fileHash, replicationFactor)
		if err != nil {
			log.Printf("WARNING: CRUSH placement failed for %s, replicating to initial daemon only: %v (errno=%d)", common.SanitizeLog(filePath), common.SanitizeLog(err.Error()), syscall.EHOSTUNREACH)
		}

		for _, node := range placement {
			if node.ID == serverId {
				continue // Already connected
			}

			peerComm, err := factory.Dial(node.Addr)
			if err != nil {
				log.Printf("Failed to connect to daemon %s: %v", node.Addr, common.SanitizeLog(err.Error()))
				continue
			}

			// Perform handshake with the other daemons
			if _, err := peerComm.HandshakeClient(authToken, timestamp, replicationMode); err != nil {
				log.Printf("Handshake failed with peer %s: %v", node.Addr, common.SanitizeLog(err.Error()))
				peerComm.Close()
				continue
			}

			communicators = append(communicators, peerComm)
		}
	}

	// Close all communicators at the end
	defer func() {
		for _, c := range communicators {
			c.Close()
		}
	}()

	// Optimization: Pre-compute file metadata (size, name) before concurrent transmission.
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		log.Printf("Failed to get file info for %s: %v", common.SanitizeLog(filePath), common.SanitizeLog(err.Error()))
		return
	}

	meta := &common.FileMetadata{
		Name:       fileInfo.Name(),
		Hash:       fileHash,
		Size:       fileInfo.Size(),
		RemotePath: remotePath,
	}

	// Validate RemotePath and length limit before transmission
	wireName := meta.Name
	if meta.RemotePath != "" {
		var normalized string
		normalized, err = common.NormalizeVirtualPath(meta.RemotePath)
		if err != nil {
			log.Printf("Failed to upload %s: invalid remote path %q: %v", common.SanitizeLog(filePath), common.SanitizeLog(meta.RemotePath), err)
			return
		}
		wireName = normalized + "/" + meta.Name
	}

	// E2EE: encrypt the wireName for metadata confidentiality.
	// Uses HMAC-SHA256 with the tenant key to produce a deterministic
	// opaque key (64 hex chars) that fits within FileInfoLength.
	if encCipher != nil {
		mac := hmac.New(sha256.New, tenantKey)
		mac.Write([]byte(wireName))
		wireName = hex.EncodeToString(mac.Sum(nil))
		meta.Name = wireName
		meta.Size = int64(len(encryptedContent))
	}

	if len(wireName) > common.FileInfoLength {
		log.Printf("Failed to upload %s: remote path and filename exceed limit of %d characters", common.SanitizeLog(filePath), common.FileInfoLength)
		return
	}

	log.Printf("=> Hash:    %s", common.SanitizeLog(meta.Hash))
	log.Printf("=> Name:    %s", common.SanitizeLog(meta.Name))
	log.Printf("=> Size:    %d", meta.Size)

	// Send the file to all established connections concurrently
	wgSendFile.Add(len(communicators))
	for _, c := range communicators {
		go sendFile(&wgSendFile, c, filePath, meta, encryptedContent)
	}
	wgSendFile.Wait()
	return
}

// sendFile sends a file over a network connection.
// It first sends the file's metadata (SHA-256 hash, name, and size) and then the file's content.
// It waits for an acknowledgment ("ACK") from the server upon successful reception.
func sendFile(wg *sync.WaitGroup, comm transport.Communicator, filePath string, meta *common.FileMetadata, encryptedContent []byte) {
	defer wg.Done()
	// 🛡️ Zero-Crash: Ensure background transmission tasks don't crash the client on unexpected panics
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in sendFile for %s: %v", common.SanitizeLog(filePath), r)
		}
	}()

	// Send metadata and receive status
	status, err := comm.SendMetadata(meta)
	if err != nil {
		log.Printf("Failed to send metadata for %s: %v", common.SanitizeLog(meta.Name), common.SanitizeLog(err.Error()))
		return
	}

	// ⚡ Bolt: Handle deduplication shortcut.
	if status == transport.MetadataStatusSkipPayload {
		log.Printf("Server already has content for %s, skipping upload.", common.SanitizeLog(meta.Name))
	} else {
		if encryptedContent != nil {
			if _, err := comm.Write(encryptedContent); err != nil {
				log.Printf("Error sending encrypted file %s: %v", common.SanitizeLog(meta.Name), common.SanitizeLog(err.Error()))
				return
			}
		} else {
			file, err := os.Open(filePath)
			if err != nil {
				log.Printf("Failed to open file %s: %v", common.SanitizeLog(filePath), common.SanitizeLog(err.Error()))
				return
			}
			if _, err := io.Copy(comm, file); err != nil {
				file.Close()
				log.Printf("Error sending file %s: %v", common.SanitizeLog(meta.Name), common.SanitizeLog(err.Error()))
				return
			}
			file.Close()
		}
	}

	// Wait for ACK
	if err := comm.ReceiveACK(); err != nil {
		log.Printf("Failed to read ACK from server: %v", common.SanitizeLog(err.Error()))
		return
	}

	log.Printf("File %s sent successfully.", common.SanitizeLog(meta.Name))
}

// ConnectStream establishes a connection with a daemon and sends file content
// from an io.Reader with pre-computed metadata. This is used by the server
// for replication forwarding when the blob may not be on the local filesystem
// (e.g., S3 or raw device backends).
func ConnectStream(wg *sync.WaitGroup, cfg common.Configuration, content io.Reader, name string, hash string, size int64, remotePath string, serverId int, timestamp int64, requestedMode int, replicationFactor int) {
	defer wg.Done()
	daemons := cfg.Daemons
	if serverId < 0 || serverId >= len(daemons) {
		log.Printf("Server ID %d is out of range", serverId)
		return
	}
	authToken := cfg.Global.AuthToken
	factory := transport.NewProtocolFactory(cfg)

	comm, err := factory.Dial(daemons[serverId].Host)
	if err != nil {
		log.Printf("Failed to connect to daemon %s: %v", daemons[serverId].Host, common.SanitizeLog(err.Error()))
		return
	}
	defer comm.Close()

	_, err = comm.HandshakeClient(authToken, timestamp, requestedMode)
	if err != nil {
		log.Printf("Handshake failed with %s: %v", daemons[serverId].Host, common.SanitizeLog(err.Error()))
		return
	}

	meta := &common.FileMetadata{
		Name:       name,
		Hash:       hash,
		Size:       size,
		RemotePath: remotePath,
	}

	wireName := meta.Name
	if meta.RemotePath != "" {
		normalized, err := common.NormalizeVirtualPath(meta.RemotePath)
		if err != nil {
			log.Printf("Failed to upload %s: invalid remote path %q: %v", common.SanitizeLog(name), common.SanitizeLog(meta.RemotePath), err)
			return
		}
		wireName = normalized + "/" + meta.Name
	}
	if len(wireName) > common.FileInfoLength {
		log.Printf("Failed to upload %s: remote path and filename exceed limit of %d characters", common.SanitizeLog(name), common.FileInfoLength)
		return
	}

	log.Printf("=> Hash:    %s", common.SanitizeLog(meta.Hash))
	log.Printf("=> Name:    %s", common.SanitizeLog(meta.Name))
	log.Printf("=> Size:    %d", meta.Size)

	sendFileStream(comm, content, meta)
}

// sendFileStream sends file content from an io.Reader over a network connection.
func sendFileStream(comm transport.Communicator, content io.Reader, meta *common.FileMetadata) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in sendFileStream for %s: %v", common.SanitizeLog(meta.Name), r)
		}
	}()

	status, err := comm.SendMetadata(meta)
	if err != nil {
		log.Printf("Failed to send metadata for %s: %v", common.SanitizeLog(meta.Name), common.SanitizeLog(err.Error()))
		return
	}

	if status == transport.MetadataStatusSkipPayload {
		log.Printf("Server already has content for %s, skipping upload.", common.SanitizeLog(meta.Name))
	} else {
		if _, err := io.Copy(comm, content); err != nil {
			log.Printf("Error sending file %s: %v", common.SanitizeLog(meta.Name), common.SanitizeLog(err.Error()))
			return
		}
	}

	if err := comm.ReceiveACK(); err != nil {
		log.Printf("Failed to read ACK from server: %v", common.SanitizeLog(err.Error()))
		return
	}

	log.Printf("File %s sent successfully.", common.SanitizeLog(meta.Name))
}

// Download retrieves an encrypted file from a daemon, decrypts it, and writes
// the plaintext to dst. The encryptedName and contentHash must be the values
// computed during upload (HMAC of wireName and SHA-256 of ciphertext).
// If encryption is disabled, the content is written directly to dst.
func Download(cfg common.Configuration, encryptedName string, contentHash string, serverId int, dst io.Writer) (err error) {
	// 🛡️ Zero-Crash: Unified panic recovery for all network-facing methods (Rule 37/43).
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in client.Download: %v", r)
			err = fmt.Errorf("panic in Download: %v: %w", r, syscall.EIO)
		}
	}()

	daemons := cfg.Daemons
	if serverId < 0 || serverId >= len(daemons) {
		return fmt.Errorf("server ID %d out of range: %w", serverId, syscall.EINVAL)
	}

	factory := transport.NewProtocolFactory(cfg)
	comm, err := factory.Dial(daemons[serverId].Host)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer comm.Close()

	// Write handshake: auth token (64) + timestamp (19) + mode byte 'G'.
	// The GET mode is sent as the literal byte 'G', not ModeGet + '0'.
	authToken := common.PadString(cfg.Global.AuthToken, common.AuthTokenLength)
	var handshakeBuf [common.AuthTokenLength + common.TimestampLength + 1]byte
	copy(handshakeBuf[:common.AuthTokenLength], authToken)
	copy(handshakeBuf[common.AuthTokenLength:], common.PadString("0", common.TimestampLength))
	handshakeBuf[common.AuthTokenLength+common.TimestampLength] = 'G'

	if _, err := comm.Write(handshakeBuf[:]); err != nil {
		return fmt.Errorf("failed to send GET handshake: %w", err)
	}

	// Send GET request: 64-byte padded name + 64-byte padded hash.
	var requestBuf [128]byte
	copy(requestBuf[:64], common.PadString(encryptedName, 64))
	copy(requestBuf[64:128], common.PadString(contentHash, 64))

	if _, err := comm.Write(requestBuf[:]); err != nil {
		return fmt.Errorf("failed to send GET request: %w", err)
	}

	// Read response: 1-byte status + 64-byte size.
	var respBuf [65]byte
	if _, err := io.ReadFull(comm, respBuf[:]); err != nil {
		return fmt.Errorf("failed to read GET response: %w", err)
	}

	if respBuf[0] != '0' {
		return fmt.Errorf("server returned error status %q: %w", respBuf[0], syscall.ENOENT)
	}

	sizeStr := common.TrimNullBytesString(respBuf[1:65])
	size, err := common.SafeParseInt([]byte(sizeStr))
	if err != nil {
		return fmt.Errorf("failed to parse file size: %w", err)
	}

	// 🛡️ Zero-Crash: Validate size against MaxFileSize to prevent unbounded
	// allocation (Rule 4). An attacker could send a maliciously large size.
	if size < 0 || size > common.MaxFileSize {
		return fmt.Errorf("file size %d exceeds maximum %d: %w", size, common.MaxFileSize, syscall.EFBIG)
	}

	// ⚡ Bolt: Stream content directly from network to decryptor to output.
	// No intermediate buffer allocation — peak memory is chunk-sized.
	if cfg.Global.EncryptionEnabled {
		cipher, err := momocrypto.NewCipherFromHex(cfg.Global.EncryptionKey)
		if err != nil {
			return fmt.Errorf("failed to create decryption cipher: %w", err)
		}
		limited := io.LimitReader(comm, size)
		if err := cipher.DecryptStream(limited, dst); err != nil {
			return fmt.Errorf("failed to decrypt content: %w", err)
		}
	} else {
		if _, err := io.CopyN(dst, comm, size); err != nil {
			return fmt.Errorf("failed to read content: %w", err)
		}
	}

	return nil
}
