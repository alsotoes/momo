// Package server provides the core functionality for the momo server.
package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"github.com/alsotoes/momo/src/transport"
)

// getMetadata reads file metadata (Hash, name, size) from a network connection.
// It reads the Hash string, file name, and file size from the connection, trims any null characters,
// and returns a FileMetadata struct.
// Null characters are trimmed because the buffers are fixed size, and the actual data may be smaller.
func getMetadata(r io.Reader) (metadata common.FileMetadata, err error) {
	defer func() {
		if rcv := recover(); rcv != nil {
			log.Printf("CRITICAL: Recovered from panic in getMetadata: %v", rcv)
			err = syscall.EIO
		}
	}()

	// ⚡ Bolt: Use a single stack-allocated buffer and single io.ReadFull call to reduce system calls and eliminate heap allocations.
	var buffer [64 + common.FileInfoLength + common.FileInfoLength]byte

	if _, err := io.ReadFull(r, buffer[:]); err != nil {
		return metadata, err
	}

	// Extract the sub-slices from the main buffer
	bufferFileHash := buffer[:64]
	bufferFileName := buffer[64 : 64+common.FileInfoLength]
	bufferFileSize := buffer[64+common.FileInfoLength:]

	// ⚡ Bolt: Use bytes.IndexByte from the standard library for finding the first null byte.
	// Re-evaluation of benchmarks on go 1.25.0 shows that bytes.IndexByte significantly
	// outperforms the manual loop for this fixed-size stack buffer.
	trimNull := func(b []byte) string {
		if idx := bytes.IndexByte(b, 0); idx != -1 {
			return string(b[:idx])
		}
		return string(b)
	}

	fileHash := trimNull(bufferFileHash)

	// 🛡️ Sentinel: Reject carriage returns or line feeds in the path to prevent HTTP Request Smuggling (CRLF Injection).
	if strings.ContainsAny(fileHash, "\r\n") {
		return metadata, fmt.Errorf("invalid hash received: contains CRLF: %w", syscall.EBADMSG)
	}

	// 🛡️ Sentinel: Sanitize Hash immediately to prevent path traversal in all downstream consumers.
	if fileHash == "" || common.HasPathTraversalChars(fileHash) {
		return metadata, fmt.Errorf("invalid hash received: %s: %w", common.SanitizeLog(fileHash), syscall.EBADMSG)
	}

	// 🛡️ Sentinel: Sanitize and normalize fileName to prevent path traversal attacks (Rule 4).
	rawFileName := trimNull(bufferFileName)

	// 🛡️ Sentinel: Reject carriage returns or line feeds in the path to prevent HTTP Request Smuggling (CRLF Injection).
	if strings.ContainsAny(rawFileName, "\r\n") {
		return metadata, fmt.Errorf("invalid characters in rawFileName: %w", syscall.EBADMSG)
	}

	// 🛡️ Sentinel: Use centralized path validation to prevent traversal and absolute paths.
	if err := common.ValidatePath(rawFileName); err != nil {
		return metadata, &os.PathError{Op: "getMetadata", Path: rawFileName, Err: os.ErrInvalid}
	}

	// ⚡ Bolt: Parse integer directly from pre-allocated buffer padding.
	fileSize, err := parsePaddedIntFast(bufferFileSize)
	if err != nil {
		return metadata, err
	}

	// 🛡️ Sentinel: Enforce maximum file size to prevent Denial of Service via resource exhaustion
	if fileSize < 0 || fileSize > common.MaxFileSize {
		return metadata, fmt.Errorf("invalid file size: %d (max: %d)", fileSize, common.MaxFileSize)
	}

	metadata.Name = rawFileName
	metadata.Hash = fileHash
	metadata.Size = fileSize

	return metadata, nil
}

// getFile reads a file from a network connection and saves it to the storage store.
func getFile(comm transport.Communicator, store storage.Store, fileName string, expectedHash string, fileSize int64, remotePath string) (err error) {
	// 🛡️ Zero-Crash: Recover from any unexpected panics in the storage backend or hash calculation.
	defer common.RecoverErr("getFile", &err)

	if store == nil {
		return fmt.Errorf("storage error: store is not initialized: %w", syscall.EIO)
	}
	// Create a TeeReader to compute SHA-256 while streaming to store. Additive
	// integrity checksums (issue #903), if a surface supplies any, are computed
	// in the same bounded-memory pass via the protocol-agnostic ChecksumSet.
	var checksumSet *common.ChecksumSet
	var mismatchHook transport.ChecksumProvider
	if p, ok := comm.(transport.ChecksumProvider); ok {
		mismatchHook = p
		if refs := p.ChecksumExpectations(); len(refs) > 0 {
			checksumSet = common.NewChecksumSetFromRefs(refs)
		}
	}
	hashCalc := sha256.New()
	var sink io.Writer = hashCalc
	if checksumSet != nil {
		sink = io.MultiWriter(hashCalc, checksumSet)
	}
	reader := io.TeeReader(comm, sink)

	// R8 quotas (#1315): charge the tenant before the bytes land, so an
	// over-quota write is refused instead of consuming disk first.
	releaseQuota, qerr := reserveTenantQuota(comm, store, fileName, fileSize)
	if qerr != nil {
		return qerr
	}

	// Use store.Put which handles deduplication and atomicity.
	if err := store.Put(fileName, expectedHash, fileSize, remotePath, io.LimitReader(reader, fileSize)); err != nil {
		releaseQuota()
		if errors.Is(err, syscall.ENOSPC) {
			reportStorageError(comm, fileName, err)
		}
		return fmt.Errorf("storage error: failed to put object %s: %w", fileName, err)
	}

	var buf [sha256.Size]byte
	hashBytes := hashCalc.Sum(buf[:0])
	var hexBuf [sha256.Size * 2]byte
	hex.Encode(hexBuf[:], hashBytes)
	hash := string(hexBuf[:])

	if err = verifyStoredDigest(store, fileName, expectedHash, hash, releaseQuota); err != nil {
		return err
	}

	// Additive integrity checksums (#820 P2 / #903), protocol-agnostic: surfaces
	// without them, or transfers with none, are inert.
	undo := func() {
		_ = store.Delete(fileName)
		releaseQuota()
	}
	if err = verifyIngestChecksums(checksumSet, mismatchHook, undo, fileName); err != nil {
		return err
	}

	// Distributed metadata write (R6 v2, #1142): the record must reach a quorum
	// of its replica set. Reporting failure lets the client retry; the local
	// copy stays, and the next write is idempotent.
	if perr := publishMetadataToQuorum(fileName, hash, fileSize, remotePath); perr != nil {
		err = fmt.Errorf("metadata replication failed for %s: %w", fileName, syscall.EIO)
		return err
	}

	log.Printf("=> Expected Hash: %s", common.SanitizeLog(expectedHash))
	log.Printf("=> Actual Hash:   %s", common.SanitizeLog(hash))
	log.Printf("Received file completely!")
	auditPut(comm, store, fileName)
	return nil
}

// verifyStoredDigest refuses an object whose stored bytes do not hash to the
// advertised digest, deleting it and undoing the reservation.
func verifyStoredDigest(store storage.Store, fileName, expectedHash, actualHash string, undo func()) error {
	if actualHash == expectedHash {
		return nil
	}
	_ = store.Delete(fileName)
	undo()
	return fmt.Errorf("file hash mismatch: expected %s, got %s: %w", expectedHash, actualHash, syscall.EBADMSG)
}

// verifyIngestChecksums refuses a payload whose additive checksums do not match
// the surface's declarations, deleting the just-stored object so no dishonest
// checksum is persisted and notifying the surface (e.g. S3 writes 400 BadDigest).
func verifyIngestChecksums(set *common.ChecksumSet, hook transport.ChecksumProvider, undo func(), fileName string) error {
	if set == nil || hook == nil || set.Verified(hook.ChecksumExpectations()) {
		return nil
	}
	undo()
	if hookErr := hook.OnIntegrityChecksumMismatch(); hookErr != nil {
		return hookErr
	}
	return fmt.Errorf("%w: %s", common.ErrIntegrityMismatch, fileName)
}

// noQuotaReserved is the release handed back when no charge was made.
var noQuotaReserved = func() {
	// Nothing was reserved, so undoing the reservation is intentionally a no-op.
}

// reserveTenantQuota charges a tenant before the payload is read and returns the
// compensating release for a write that never becomes an object. It is a no-op
// (returning a harmless release) for tenant-less surfaces and stores that do not
// implement quota accounting. An overwrite pays only the size delta and never
// consumes another object.
func reserveTenantQuota(comm transport.Communicator, store storage.Store, fileName string, fileSize int64) (release func(), err error) {
	tenantID := tenantOf(comm)
	quota, hasQuota := store.(tenantQuotaAdjuster)
	if !hasQuota || tenantID == "" {
		return noQuotaReserved, nil
	}
	var deltaBytes, deltaObjects int64
	if meta, merr := store.GetMeta(fileName); merr == nil {
		deltaBytes = fileSize - meta.Size
	} else {
		deltaBytes, deltaObjects = fileSize, 1
	}
	if qerr := quota.AdjustTenantQuota(tenantID, deltaBytes, deltaObjects); qerr != nil {
		reportStorageError(comm, fileName, qerr)
		return noQuotaReserved, fmt.Errorf("tenant quota for %s: %w", fileName, qerr)
	}
	return func() { releaseTenantQuota(tenantID, quota, fileName, deltaBytes, deltaObjects) }, nil
}

// releaseTenantQuota reverses a reservation after the object was not persisted.
func releaseTenantQuota(tenantID string, quota tenantQuotaAdjuster, fileName string, deltaBytes, deltaObjects int64) {
	if deltaBytes == 0 && deltaObjects == 0 {
		return
	}
	if rerr := quota.AdjustTenantQuota(tenantID, -deltaBytes, -deltaObjects); rerr != nil {
		log.Printf("AUDIT: failed to release tenant quota for %s: %v", fileName, common.SanitizeLog(rerr.Error()))
	}
}

// reportStorageError lets the surface translate a storage refusal (e.g. ENOSPC,
// quota) into its own wire error. Failures are only logged: the request already
// failed, and a reporting failure must not mask the original error.
func reportStorageError(comm transport.Communicator, fileName string, cause error) {
	if rerr := comm.OnStorageError(cause); rerr != nil {
		log.Printf("AUDIT: OnStorageError failed for %s: %v", fileName, common.SanitizeLog(rerr.Error()))
	}
}

// tenantQuotaAdjuster is the optional storage seam for per-tenant quota
// accounting (#1315). Stores without it leave quotas unenforced.
type tenantQuotaAdjuster interface {
	AdjustTenantQuota(tenantID string, deltaBytes, deltaObjects int64) error
}

// tenantOf returns the authenticated tenant for a tenant-aware surface (the S3
// gateway), or "" for surfaces that carry no tenant identity.
func tenantOf(comm transport.Communicator) string {
	if tp, ok := comm.(interface{ TenantID() string }); ok {
		return tp.TenantID()
	}
	return ""
}

// auditPut records a successful write in the tamper-evident audit log (R8,
// #936). The tenant is only present on the tenant-authenticated S3 surface;
// peer and native writes record an empty tenant. Failures are logged, never
// fatal, so auditing can never break the write path.
func auditPut(comm transport.Communicator, store storage.Store, fileName string) {
	tenantID := tenantOf(comm)
	entry := &common.AuditLogEntry{
		TenantID:  tenantID,
		Identity:  "put",
		Operation: "PutObject",
		Resource:  fileName,
		Outcome:   "success",
		RequestID: strconv.FormatInt(time.Now().UnixNano(), 10),
	}
	if err := store.WriteAuditLog(entry); err != nil {
		log.Printf("AUDIT: Failed to write audit log for %s: %v", common.SanitizeLog(fileName), err)
	}
}

// parsePaddedIntFast parses a null-padded or null-terminated byte slice into an int64
// without allocating an intermediate string.
func parsePaddedIntFast(b []byte) (int64, error) {
	return common.SafeParseInt(b)
}
