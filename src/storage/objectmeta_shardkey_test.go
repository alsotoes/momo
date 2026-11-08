package storage

import (
	"testing"
)

// TestObjectMetaRoundTripWithShardKey guards the v2 encoding: the shard-key
// length header was allocated with the payload size and appended payload-sized
// zero bytes, shifting the replica list and failing on every non-empty shard
// key (found via the R6 quorum write path, #1142).
func TestObjectMetaRoundTripWithShardKey(t *testing.T) {
	cases := []string{"", "a", "ê", "shard-42", "e\u0301x"}
	for _, sk := range cases {
		in := ObjectMeta{
			Size:             7,
			RefCount:         2,
			DeletedAt:        0,
			VectorClock:      []uint64{1, 2},
			ShardKey:         sk,
			MetadataReplicas: []int32{0, 1, 2},
		}
		out, err := decodeObjectMeta(in.encode())
		if err != nil {
			t.Fatalf("shard key %q: decode: %v", sk, err)
		}
		if out.ShardKey != sk {
			t.Fatalf("shard key %q round-tripped as %q", sk, out.ShardKey)
		}
		if out.Size != in.Size || out.RefCount != in.RefCount {
			t.Fatalf("shard key %q: size/refcount mismatch: %+v", sk, out)
		}
		if len(out.VectorClock) != 2 || out.VectorClock[1] != 2 {
			t.Fatalf("shard key %q: vector clock mismatch: %v", sk, out.VectorClock)
		}
		if len(out.MetadataReplicas) != 3 || out.MetadataReplicas[2] != 2 {
			t.Fatalf("shard key %q: replicas mismatch: %v", sk, out.MetadataReplicas)
		}
	}
}

// TestPutWithMetadataSurvivesNonEmptyShardKey proves the fix end to end
// through the public store API.
func TestPutWithMetadataSurvivesNonEmptyShardKey(t *testing.T) {
	s, err := NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.PutWithMetadata("obj", "hash-shard", 0, "", nil, []uint64{1}, "ê", []int32{0, 1}); err != nil {
		t.Fatalf("PutWithMetadata: %v", err)
	}
	if _, err := s.GetMeta("obj"); err != nil {
		t.Fatalf("GetMeta after a non-empty shard key: %v", err)
	}
}
