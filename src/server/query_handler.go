package server

import (
	"encoding/binary"
	"fmt"
	"syscall"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/p2p"
	"github.com/alsotoes/momo/src/storage"
)

// StorageQueryHandler implements p2p.QueryHandler over the local CAS store.
// It enables scatter-gather queries for list/get/has operations across the cluster.
type StorageQueryHandler struct {
	store storage.Store
}

// NewStorageQueryHandler creates a new StorageQueryHandler.
func NewStorageQueryHandler(store storage.Store) *StorageQueryHandler {
	return &StorageQueryHandler{store: store}
}

// HandleQuery processes a scatter-gather query locally and returns the encoded result.
func (h *StorageQueryHandler) HandleQuery(qt p2p.QueryType, data []byte) ([]byte, error) {
	switch qt {
	case p2p.QueryList:
		return h.handleList()
	case p2p.QueryGet:
		return h.handleGet(data)
	case p2p.QueryHas:
		return h.handleHas(data)
	case p2p.QueryDelete:
		return h.handleDelete(data)
	default:
		return nil, fmt.Errorf("unknown query type: %d", qt)
	}
}

// handleList returns all local files as binary-encoded FileMetadata list.
func (h *StorageQueryHandler) handleList() ([]byte, error) {
	files, err := h.store.List()
	if err != nil {
		return nil, err
	}
	return EncodeFileMetadataList(files), nil
}

// handleGet returns metadata for a specific file.
func (h *StorageQueryHandler) handleGet(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty file name")
	}
	name := string(data)
	_, meta, err := h.store.Get(name)
	if err != nil {
		return nil, err
	}
	return EncodeFileMetadataList([]common.FileMetadata{meta}), nil
}

// handleHas checks if a hash exists in the local store.
func (h *StorageQueryHandler) handleHas(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty hash")
	}
	hash := string(data)
	exists, err := h.store.Has(hash)
	if err != nil {
		return nil, err
	}
	result := make([]byte, 1)
	if exists {
		result[0] = 1
	}
	return result, nil
}

// handleDelete deletes a file by name from the local store.
// This is invoked by remote peers via scatter-gather to propagate deletes.
func (h *StorageQueryHandler) handleDelete(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty file name: %w", syscall.EINVAL)
	}
	name := string(data)
	if err := h.store.Delete(name); err != nil {
		return nil, err
	}
	result := make([]byte, 1)
	result[0] = 1
	return result, nil
}

// EncodeFileMetadataList serializes a list of FileMetadata into binary.
// Format: [4B count] [for each: 4B nameLen + name + 4B hashLen + hash + 8B size + 4B pathLen + path]
func EncodeFileMetadataList(files []common.FileMetadata) []byte {
	size := 4
	for _, f := range files {
		size += 4 + len(f.Name) + 4 + len(f.Hash) + 8 + 4 + len(f.RemotePath)
	}
	buf := make([]byte, size)
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(files)))
	off := 4
	for _, f := range files {
		binary.BigEndian.PutUint32(buf[off:off+4], uint32(len(f.Name)))
		off += 4
		copy(buf[off:], f.Name)
		off += len(f.Name)
		binary.BigEndian.PutUint32(buf[off:off+4], uint32(len(f.Hash)))
		off += 4
		copy(buf[off:], f.Hash)
		off += len(f.Hash)
		binary.BigEndian.PutUint64(buf[off:off+8], uint64(f.Size))
		off += 8
		binary.BigEndian.PutUint32(buf[off:off+4], uint32(len(f.RemotePath)))
		off += 4
		copy(buf[off:], f.RemotePath)
		off += len(f.RemotePath)
	}
	return buf
}

// DecodeFileMetadataList deserializes a binary FileMetadata list.
func DecodeFileMetadataList(data []byte) ([]common.FileMetadata, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("file metadata list too short")
	}
	count := int(binary.BigEndian.Uint32(data[0:4]))
	off := 4
	files := make([]common.FileMetadata, 0, count)
	for i := 0; i < count; i++ {
		if off+4 > len(data) {
			return nil, fmt.Errorf("truncated name length at entry %d", i)
		}
		nameLen := int(binary.BigEndian.Uint32(data[off : off+4]))
		off += 4
		if off+nameLen > len(data) {
			return nil, fmt.Errorf("truncated name at entry %d", i)
		}
		name := string(data[off : off+nameLen])
		off += nameLen

		if off+4 > len(data) {
			return nil, fmt.Errorf("truncated hash length at entry %d", i)
		}
		hashLen := int(binary.BigEndian.Uint32(data[off : off+4]))
		off += 4
		if off+hashLen > len(data) {
			return nil, fmt.Errorf("truncated hash at entry %d", i)
		}
		hash := string(data[off : off+hashLen])
		off += hashLen

		if off+8 > len(data) {
			return nil, fmt.Errorf("truncated size at entry %d", i)
		}
		fileSize := int64(binary.BigEndian.Uint64(data[off : off+8]))
		off += 8

		if off+4 > len(data) {
			return nil, fmt.Errorf("truncated path length at entry %d", i)
		}
		pathLen := int(binary.BigEndian.Uint32(data[off : off+4]))
		off += 4
		if off+pathLen > len(data) {
			return nil, fmt.Errorf("truncated path at entry %d", i)
		}
		remotePath := string(data[off : off+pathLen])
		off += pathLen

		files = append(files, common.FileMetadata{
			Name:       name,
			Hash:       hash,
			Size:       fileSize,
			RemotePath: remotePath,
		})
	}
	return files, nil
}

// MergeFileMetadataLists merges multiple file lists and deduplicates by hash.
func MergeFileMetadataLists(lists ...[]common.FileMetadata) []common.FileMetadata {
	seen := make(map[string]bool)
	var merged []common.FileMetadata
	for _, list := range lists {
		for _, f := range list {
			if !seen[f.Hash] {
				seen[f.Hash] = true
				merged = append(merged, f)
			}
		}
	}
	return merged
}
