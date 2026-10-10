package engine_test

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/Arjun7114/modelvault/internal/backend"
	"github.com/Arjun7114/modelvault/internal/chunker"
	"github.com/Arjun7114/modelvault/internal/engine"
)

func makeBytes(n int, seed int64) []byte {
	r := rand.New(rand.NewSource(seed))
	b := make([]byte, n)
	r.Read(b)
	return b
}

// The concurrent path must produce byte-for-byte the same manifest and the same
// stored chunks as the serial path — same hashes, same order, same stats.
func TestBackupConcurrent_MatchesSerial(t *testing.T) {
	data := makeBytes(100_000, 99)

	beA, _ := backend.NewLocal(t.TempDir())
	snapA, statsA, err := engine.New(chunker.NewFixed(1024), beA).
		Backup("x", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}

	beB, _ := backend.NewLocal(t.TempDir())
	snapB, statsB, err := engine.New(chunker.NewFixed(1024), beB).
		BackupConcurrent("x", bytes.NewReader(data), 8)
	if err != nil {
		t.Fatalf("concurrent: %v", err)
	}

	if len(snapA.Chunks) != len(snapB.Chunks) {
		t.Fatalf("chunk count: serial %d, concurrent %d", len(snapA.Chunks), len(snapB.Chunks))
	}
	for i := range snapA.Chunks {
		if snapA.Chunks[i] != snapB.Chunks[i] {
			t.Fatalf("chunk %d hash differs (ordering bug): serial %s, concurrent %s",
				i, snapA.Chunks[i], snapB.Chunks[i])
		}
	}
	if statsA.TotalChunks != statsB.TotalChunks || statsA.NewChunks != statsB.NewChunks {
		t.Errorf("stats differ: serial %+v, concurrent %+v", statsA, statsB)
	}
}

// A concurrent backup must still restore byte-for-byte.
func TestBackupConcurrent_RoundTrip(t *testing.T) {
	be, _ := backend.NewLocal(t.TempDir())
	eng := engine.New(chunker.NewFixed(1024), be)

	original := makeBytes(50_000, 7)
	snap, _, err := eng.BackupConcurrent("x", bytes.NewReader(original), 8)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	var buf bytes.Buffer
	if _, err := eng.Restore(snap.ID, &buf); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), original) {
		t.Errorf("round-trip mismatch: got %d bytes, want %d", buf.Len(), len(original))
	}
}