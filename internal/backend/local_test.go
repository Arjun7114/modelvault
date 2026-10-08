package backend_test

import (
	"bytes"
	"testing"

	"github.com/Arjun7114/modelvault/internal/backend"
)

func TestLocalBackend_ChunkRoundTrip(t *testing.T) {
	b, err := backend.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}

	hash, data := "deadbeef", []byte("hello chunk")

	if ok, err := b.HasChunk(hash); err != nil || ok {
		t.Fatalf("HasChunk before put = (%v, %v), want (false, nil)", ok, err)
	}
	if err := b.PutChunk(hash, data); err != nil {
		t.Fatalf("PutChunk: %v", err)
	}
	if ok, err := b.HasChunk(hash); err != nil || !ok {
		t.Fatalf("HasChunk after put = (%v, %v), want (true, nil)", ok, err)
	}

	got, err := b.GetChunk(hash)
	if err != nil {
		t.Fatalf("GetChunk: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("GetChunk = %q, want %q", got, data)
	}

	// Putting the same hash again is a successful no-op (dedup).
	if err := b.PutChunk(hash, data); err != nil {
		t.Errorf("second PutChunk returned error: %v", err)
	}
}

func TestLocalBackend_Snapshots(t *testing.T) {
	b, err := backend.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}

	if err := b.PutSnapshot("snap1", []byte(`{"id":"snap1"}`)); err != nil {
		t.Fatalf("PutSnapshot: %v", err)
	}
	got, err := b.GetSnapshot("snap1")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if string(got) != `{"id":"snap1"}` {
		t.Errorf("GetSnapshot = %q", got)
	}

	ids, err := b.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(ids) != 1 || ids[0] != "snap1" {
		t.Errorf("ListSnapshots = %v, want [snap1]", ids)
	}
}

func TestLocalBackend_RejectsUnsafeKey(t *testing.T) {
	b, err := backend.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	if err := b.PutChunk("../escape", []byte("x")); err == nil {
		t.Error("expected error for unsafe key, got nil")
	}
}