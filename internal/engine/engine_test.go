package engine_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Arjun7114/modelvault/internal/backend"
	"github.com/Arjun7114/modelvault/internal/chunker"
	"github.com/Arjun7114/modelvault/internal/engine"
)

func TestBackup_DeduplicatesRepeatedChunks(t *testing.T) {
	ctx := context.Background()
	be, err := backend.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	eng := engine.New(chunker.NewFixed(4), be)

	snap, stats, err := eng.Backup(ctx, "test", strings.NewReader("AAAABBBBAAAA"))
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	if stats.TotalChunks != 3 {
		t.Errorf("TotalChunks = %d, want 3", stats.TotalChunks)
	}
	if stats.NewChunks != 2 {
		t.Errorf("NewChunks = %d, want 2", stats.NewChunks)
	}
	if stats.DupChunks != 1 {
		t.Errorf("DupChunks = %d, want 1", stats.DupChunks)
	}
	if len(snap.Chunks) != 3 {
		t.Errorf("snapshot has %d chunk refs, want 3", len(snap.Chunks))
	}
	if _, err := be.GetSnapshot(ctx, snap.ID); err != nil {
		t.Errorf("GetSnapshot(%q): %v", snap.ID, err)
	}
}

func TestBackup_AcrossTwoRuns(t *testing.T) {
	ctx := context.Background()
	be, err := backend.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	eng := engine.New(chunker.NewFixed(4), be)

	_, s1, err := eng.Backup(ctx, "v1", strings.NewReader("AAAABBBB"))
	if err != nil {
		t.Fatalf("first Backup: %v", err)
	}
	if s1.NewChunks != 2 {
		t.Errorf("first run NewChunks = %d, want 2", s1.NewChunks)
	}

	_, s2, err := eng.Backup(ctx, "v2", strings.NewReader("AAAABBBB"))
	if err != nil {
		t.Fatalf("second Backup: %v", err)
	}
	if s2.NewChunks != 0 {
		t.Errorf("second run NewChunks = %d, want 0", s2.NewChunks)
	}
	if s2.DupChunks != 2 {
		t.Errorf("second run DupChunks = %d, want 2", s2.DupChunks)
	}
}

func TestBackupRestore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	be, err := backend.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	eng := engine.New(chunker.NewFixed(4), be)

	original := "the quick brown fox jumps over the lazy dog"
	snap, _, err := eng.Backup(ctx, "doc", strings.NewReader(original))
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	var buf bytes.Buffer
	stats, err := eng.Restore(ctx, snap.ID, &buf)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if buf.String() != original {
		t.Errorf("restored = %q, want %q", buf.String(), original)
	}
	if stats.Bytes != int64(len(original)) {
		t.Errorf("restored %d bytes, want %d", stats.Bytes, len(original))
	}
}