package backend_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/Arjun7114/modelvault/internal/backend"
)

// TestAzureBackend_Integration runs only when AZURE_STORAGE_CONNECTION_STRING is
// set, so the normal suite doesn't require Azure.
func TestAzureBackend_Integration(t *testing.T) {
	conn := os.Getenv("AZURE_STORAGE_CONNECTION_STRING")
	if conn == "" {
		t.Skip("set AZURE_STORAGE_CONNECTION_STRING to run the Azure integration test")
	}
	container := os.Getenv("MODELVAULT_AZURE_CONTAINER")
	if container == "" {
		container = "modelvault"
	}
	ctx := context.Background()

	b, err := backend.NewAzure(conn, container)
	if err != nil {
		t.Fatalf("NewAzure: %v", err)
	}

	hash := "testchunk0123456789abcdef"
	data := []byte("hello from modelvault azure test")

	if err := b.PutChunk(ctx, hash, data); err != nil {
		t.Fatalf("PutChunk: %v", err)
	}
	if ok, err := b.HasChunk(ctx, hash); err != nil || !ok {
		t.Fatalf("HasChunk = (%v, %v), want (true, nil)", ok, err)
	}
	got, err := b.GetChunk(ctx, hash)
	if err != nil {
		t.Fatalf("GetChunk: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("GetChunk = %q, want %q", got, data)
	}

	if ok, err := b.HasChunk(ctx, "definitelymissingchunk999"); err != nil {
		t.Fatalf("HasChunk(missing): %v", err)
	} else if ok {
		t.Error("HasChunk(missing) = true, want false")
	}

	id := "testsnap0123456789"
	if err := b.PutSnapshot(ctx, id, []byte(`{"id":"testsnap"}`)); err != nil {
		t.Fatalf("PutSnapshot: %v", err)
	}
	snapData, err := b.GetSnapshot(ctx, id)
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if string(snapData) != `{"id":"testsnap"}` {
		t.Errorf("GetSnapshot = %q", snapData)
	}

	ids, err := b.ListSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	found := false
	for _, x := range ids {
		if x == id {
			found = true
		}
	}
	if !found {
		t.Errorf("ListSnapshots did not contain %q; got %v", id, ids)
	}
}