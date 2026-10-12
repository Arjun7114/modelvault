package engine_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Arjun7114/modelvault/internal/backend"
	"github.com/Arjun7114/modelvault/internal/chunker"
	"github.com/Arjun7114/modelvault/internal/engine"
)

func TestVerify_PassesThenDetectsCorruption(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	be, err := backend.NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(chunker.NewFixed(16), be)

	data := makeBytes(1000, 4)
	snap, _, err := eng.Backup(ctx, "x", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// Clean data verifies OK.
	if _, err := eng.Verify(ctx, snap.ID); err != nil {
		t.Fatalf("Verify on clean data: %v", err)
	}

	// Corrupt the first chunk on disk.
	chunkPath := filepath.Join(root, "chunks", snap.Chunks[0])
	if err := os.WriteFile(chunkPath, []byte("corrupted!"), 0o644); err != nil {
		t.Fatalf("corrupting chunk: %v", err)
	}

	// Verify must now fail.
	if _, err := eng.Verify(ctx, snap.ID); err == nil {
		t.Fatal("expected Verify to detect corruption, got nil")
	}
}