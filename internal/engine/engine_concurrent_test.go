package engine_test

import (
	"bytes"
	"context"
	"errors"
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

// The concurrent path must produce byte-for-byte the same manifest and stored
// chunks as the serial path — same hashes, same order, same stats.
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
		BackupConcurrent(context.Background(), "x", bytes.NewReader(data), 8)
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
	snap, _, err := eng.BackupConcurrent(context.Background(), "x", bytes.NewReader(original), 8)
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

// With lots of repeated blocks, dedup counters are heavily exercised. The
// concurrent path must produce identical stats and manifest to the serial path.
func TestBackupConcurrent_DedupMatchesSerial_WithRepeats(t *testing.T) {
	block := makeBytes(1024, 1)
	var data []byte
	for i := 0; i < 50; i++ {
		data = append(data, block...)
	}
	data = append(data, makeBytes(2048, 2)...)

	beA, _ := backend.NewLocal(t.TempDir())
	snapA, sA, err := engine.New(chunker.NewFixed(1024), beA).Backup("x", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}

	beB, _ := backend.NewLocal(t.TempDir())
	snapB, sB, err := engine.New(chunker.NewFixed(1024), beB).
		BackupConcurrent(context.Background(), "x", bytes.NewReader(data), 8)
	if err != nil {
		t.Fatalf("concurrent: %v", err)
	}

	if sA != sB {
		t.Errorf("stats differ:\n  serial     %+v\n  concurrent %+v", sA, sB)
	}
	if len(snapA.Chunks) != len(snapB.Chunks) {
		t.Fatalf("chunk count: serial %d, concurrent %d", len(snapA.Chunks), len(snapB.Chunks))
	}
	for i := range snapA.Chunks {
		if snapA.Chunks[i] != snapB.Chunks[i] {
			t.Fatalf("chunk %d differs", i)
		}
	}
}

// A cancelled context must abort the backup and surface context.Canceled.
func TestBackupConcurrent_Cancellation(t *testing.T) {
	be, _ := backend.NewLocal(t.TempDir())
	eng := engine.New(chunker.NewFixed(1024), be)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before starting

	_, _, err := eng.BackupConcurrent(ctx, "x", bytes.NewReader(makeBytes(1_000_000, 5)), 8)
	if err == nil {
		t.Fatal("expected a cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
// A concurrent restore must reproduce the original bytes exactly.
func TestRestoreConcurrent_RoundTrip(t *testing.T) {
	be, _ := backend.NewLocal(t.TempDir())
	eng := engine.New(chunker.NewFixed(1024), be)

	original := makeBytes(80_000, 11)
	snap, _, err := eng.BackupConcurrent(context.Background(), "x", bytes.NewReader(original), 8)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	var buf bytes.Buffer
	stats, err := eng.RestoreConcurrent(context.Background(), snap.ID, &buf, 8)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), original) {
		t.Errorf("round-trip mismatch: got %d bytes, want %d", buf.Len(), len(original))
	}
	if stats.Bytes != int64(len(original)) {
		t.Errorf("restored %d bytes, want %d", stats.Bytes, len(original))
	}
}

// A cancelled context must abort the restore and surface context.Canceled.
func TestRestoreConcurrent_Cancellation(t *testing.T) {
	be, _ := backend.NewLocal(t.TempDir())
	eng := engine.New(chunker.NewFixed(1024), be)

	snap, _, err := eng.BackupConcurrent(context.Background(), "x", bytes.NewReader(makeBytes(200_000, 3)), 8)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = eng.RestoreConcurrent(ctx, snap.ID, &bytes.Buffer{}, 8)
	if err == nil {
		t.Fatal("expected a cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}