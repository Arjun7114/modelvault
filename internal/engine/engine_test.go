package engine_test

import (
	"strings"
	"testing"

	"github.com/Arjun7114/modelvault/internal/backend"
	"github.com/Arjun7114/modelvault/internal/chunker"
	"github.com/Arjun7114/modelvault/internal/engine"
)

func TestBackup_DeduplicatesRepeatedChunks(t *testing.T) {
	be, err := backend.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	eng := engine.New(chunker.NewFixed(4), be)

	// "AAAABBBBAAAA" at size 4 -> AAAA, BBBB, AAAA: 3 chunks, 2 unique.
	snap, stats, err := eng.Backup("test", strings.NewReader("AAAABBBBAAAA"))
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
	if _, err := be.GetSnapshot(snap.ID); err != nil {
		t.Errorf("GetSnapshot(%q): %v", snap.ID, err)
	}
}

func TestBackup_AcrossTwoRuns(t *testing.T) {
	be, err := backend.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	eng := engine.New(chunker.NewFixed(4), be)

	// First backup: everything is new.
	_, s1, err := eng.Backup("v1", strings.NewReader("AAAABBBB"))
	if err != nil {
		t.Fatalf("first Backup: %v", err)
	}
	if s1.NewChunks != 2 {
		t.Errorf("first run NewChunks = %d, want 2", s1.NewChunks)
	}

	// Second backup of the SAME data: everything is a duplicate, nothing new.
	_, s2, err := eng.Backup("v2", strings.NewReader("AAAABBBB"))
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