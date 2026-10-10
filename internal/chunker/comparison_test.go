package chunker_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/Arjun7114/modelvault/internal/chunker"
)

// hashChunks returns the SHA-256 (hex) of each chunk — i.e. the address each
// chunk would be stored under. This is exactly what the backend dedups on.
func hashChunks(chunks [][]byte) []string {
	out := make([]string, len(chunks))
	for i, c := range chunks {
		sum := sha256.Sum256(c)
		out[i] = hex.EncodeToString(sum[:])
	}
	return out
}

// countNew returns how many chunk hashes in v2 did NOT appear in v1 — i.e. the
// number of chunks a second backup would actually have to store.
func countNew(v1, v2 []string) int {
	seen := make(map[string]bool, len(v1))
	for _, h := range v1 {
		seen[h] = true
	}
	n := 0
	for _, h := range v2 {
		if !seen[h] {
			n++
		}
	}
	return n
}

// insertAt returns a copy of data with extra inserted at the given offset.
func insertAt(data []byte, offset int, extra []byte) []byte {
	out := make([]byte, 0, len(data)+len(extra))
	out = append(out, data[:offset]...)
	out = append(out, extra...)
	out = append(out, data[offset:]...)
	return out
}

// This is the headline experiment of Phase 2: take a 1 MiB file, insert 200
// bytes near the front, and measure how many chunks each strategy must re-store
// on the second backup. Fixed-size chunking shifts every boundary after the
// edit, so it re-stores almost everything; content-defined chunking only
// disturbs the chunks around the edit, so it re-stores almost nothing.
//
// Run with:  go test ./internal/chunker -run Comparison -v
func TestChunkerComparison_InsertionResilience(t *testing.T) {
	const size = 1 << 20 // 1 MiB
	base := makeData(size, 42)
	modified := insertAt(base, 1000, makeData(200, 7)) // 200 bytes inserted near the front

	cdc, err := chunker.NewCDC(1024, 12, 16384)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		c    chunker.Chunker
	}{
		{"fixed-4096", chunker.NewFixed(4096)},
		{"cdc-avg4096", cdc},
	}

	restored := make(map[string]int)

	t.Logf("%-12s %8s %8s %12s %8s", "chunker", "v1", "v2", "re-stored", "reuse%")
	for _, tc := range cases {
		v1 := hashChunks(splitAll(t, tc.c, base))
		v2 := hashChunks(splitAll(t, tc.c, modified))
		newCount := countNew(v1, v2)
		restored[tc.name] = newCount
		reuse := 100.0 * float64(len(v2)-newCount) / float64(len(v2))
		t.Logf("%-12s %8d %8d %12d %7.1f%%", tc.name, len(v1), len(v2), newCount, reuse)
	}

	// The core guarantee of this phase: CDC re-stores far fewer chunks.
	if restored["cdc-avg4096"] >= restored["fixed-4096"] {
		t.Errorf("expected CDC to re-store fewer chunks than fixed: cdc=%d, fixed=%d",
			restored["cdc-avg4096"], restored["fixed-4096"])
	}
}