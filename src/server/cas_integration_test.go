package server

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"github.com/alsotoes/momo/src/transport"
	"go.uber.org/goleak"
)

// TestCAS_MultiNode_Integration simulates a 5-node cluster with replication factor 3.
// It verifies CRUSH placement, deduplication, and Bbolt metadata persistence.
func TestCAS_MultiNode_Integration(t *testing.T) {
	defer goleak.VerifyNone(t)

	// 1. Setup 5 virtual nodes
	nodeCount := 5
	replFactor := 3
	tempDir := t.TempDir()

	daemons := make([]*common.Daemon, nodeCount)
	stores := make([]storage.Store, nodeCount)
	listeners := make([]net.Listener, nodeCount)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret

	for i := 0; i < nodeCount; i++ {
		nodeData := filepath.Join(tempDir, fmt.Sprintf("node-%d", i))
		os.MkdirAll(nodeData, 0755)

		// Create local Bbolt store for this node
		store, err := storage.NewCASStore(nodeData)
		if err != nil {
			t.Fatalf("Failed to create store for node %d: %v", i, err)
		}
		stores[i] = store
		defer store.Close()

		// Bind a local listener
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to listen for node %d: %v", i, err)
		}
		listeners[i] = ln
		defer ln.Close()

		daemons[i] = &common.Daemon{
			Host: ln.Addr().String(),
			Data: nodeData,
		}
	}

	// Start virtual daemon goroutines
	var daemonsWg sync.WaitGroup
	for i := 0; i < nodeCount; i++ {
		id := i
		ln := listeners[i]
		store := stores[i]

		daemonsWg.Add(1)
		go func() {
			defer daemonsWg.Done()

			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}

				// Handle connection
				func(c net.Conn) {
					comm := transport.NewMomoTCPCommunicator(c)
					defer comm.Close()

					// Handshake
					expectedAuth := []byte(common.PadString(authToken, 64))
					_, _, err := comm.HandshakeServer(expectedAuth)
					if err != nil {
						return
					}
					comm.SendReplicationMode(common.ReplicationNone) // Simple integration test

					meta, err := comm.ReceiveMetadata()
					if err != nil {
						return
					}

					// Check deduplication (CVE-005: require proof-of-knowledge for new names)
					exists, _ := store.Has(meta.Hash)
					canDedup := false
					if exists {
						existingHash, hashErr := store.GetHashForName(meta.Name)
						if hashErr == nil && existingHash == meta.Hash {
							canDedup = true
						}
					}
					if canDedup {
						comm.SendMetadataStatus(transport.MetadataStatusSkipPayload)
						// Update metadata only
						store.Put(meta.Name, meta.Hash, meta.Size, "", nil)
					} else {
						comm.SendMetadataStatus(transport.MetadataStatusSendPayload)
						getFile(comm, store, meta.Name, meta.Hash, meta.Size, "")
					}
					comm.SendACK(id)
				}(conn)
			}
		}()
	}

	// 2. Simulate Client sending tiny files
	files := []struct {
		name        string
		content     string
		expectDedup bool
	}{
		{"file1.txt", "tiny content 1", false},
		{"file2.txt", "different content", false},
		{"file1.txt", "tiny content 1", true},      // Same name, same content → dedup
		{"duplicate.txt", "tiny content 1", false}, // New name, same content → no dedup (CVE-005)
	}

	// Pre-build ClusterMap
	nodes := make([]*common.Node, nodeCount)
	for i, d := range daemons {
		nodes[i] = &common.Node{ID: i, Weight: 1, Addr: d.Host}
	}
	cmap := &common.ClusterMap{Nodes: nodes}

	for _, f := range files {
		content := []byte(f.content)
		hash := common.HashBytes(content)

		// Calculate Primary using CRUSH
		placement, _ := cmap.Placement(hash, replFactor)
		primary := placement[0]

		t.Logf("File %s (hash: %s) -> Primary Node: %d", f.name, hash[:8], primary.ID)

		// Connect to primary
		conn, err := net.Dial("tcp", primary.Addr)
		if err != nil {
			t.Fatalf("Failed to dial node %d: %v", primary.ID, err)
		}
		comm := transport.NewMomoTCPCommunicator(conn)

		// Handshake
		_, err = comm.HandshakeClient(authToken, common.DummyEpoch, 0)
		if err != nil {
			t.Fatalf("Handshake failed: %v", err)
		}

		// Send Metadata
		meta := &common.FileMetadata{Name: f.name, Hash: hash, Size: int64(len(content))}
		status, err := comm.SendMetadata(meta)
		if err != nil {
			t.Fatalf("SendMetadata failed: %v", err)
		}

		if f.expectDedup {
			if status != transport.MetadataStatusSkipPayload {
				t.Errorf("Expected SkipPayload for %s (dedup), got %d", f.name, status)
			}
		} else {
			if status != transport.MetadataStatusSendPayload {
				t.Errorf("Expected SendPayload for %s, got %d", f.name, status)
			}
			// Send payload
			comm.Write(content)
		}

		// Wait for ACK
		if err := comm.ReceiveACK(); err != nil {
			t.Errorf("ReceiveACK failed for %s: %v", f.name, err)
		}
		comm.Close()
	}

	// Close all listeners to unblock Accept() calls and shut down daemons
	for _, ln := range listeners {
		ln.Close()
	}
	daemonsWg.Wait()

	// 3. Final Verification
	file1Hash := common.HashBytes([]byte("tiny content 1"))
	foundInObjects := false
	foundInNamespace := false

	for i := 0; i < nodeCount; i++ {
		exists, _ := stores[i].Has(file1Hash)
		if exists {
			foundInObjects = true
			// Check if the blob is readable
			reader, _, err := stores[i].Get("file1.txt")
			if err != nil {
				t.Errorf("Blob not readable on node %d: %v", i, err)
			} else {
				reader.Close()
			}

			// Verify duplicate.txt also maps here
			_, m, err := stores[i].Get("duplicate.txt")
			if err == nil && m.Hash == file1Hash {
				foundInNamespace = true
			}
		}
	}

	if !foundInObjects {
		t.Errorf("Content hash %s not found in any node", file1Hash)
	}
	if !foundInNamespace {
		t.Errorf("duplicate.txt mapping not found in any node")
	}
}
