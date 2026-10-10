package chunker_test

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/Arjun7114/modelvault/internal/chunker"
)

// makeData returns n deterministic pseudo-random bytes for a given seed.
func makeData(n int, seed int64) []byte {
	r := rand.New(rand.NewSource(seed))
	b := make([]byte, n)
	r.Read(b)
	return b
}

func splitAll(t *testing.T, c chunker.Chunker, data []byte) [][]byte {
	t.Helper()
	var chunks [][]byte
	err := c.Split(bytes.NewReader(data), func(chunk []byte) error {
		cp := make([]byte, len(chunk))
		copy(cp, chunk)
		chunks = append(chunks, cp)
		return nil
	})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	return chunks
}

// The chunks must reassemble into exactly the original bytes, and there must be
// more than one of them (so we're actually exercising boundary detection).
func TestCDC_Completeness(t *testing.T) {
	c, err := chunker.NewCDC(16, 5, 256)
	if err != nil {
		t.Fatal(err)
	}
	data := makeData(10000, 1)
	chunks := splitAll(t, c, data)

	var joined []byte
	for _, ch := range chunks {
		joined = append(joined, ch...)
	}
	if !bytes.Equal(joined, data) {
		t.Fatalf("rejoined chunks != original (got %d bytes, want %d)", len(joined), len(data))
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
}

// Every chunk must be <= max; every chunk except the last must be >= min.
func TestCDC_SizeBounds(t *testing.T) {
	min, max := 16, 256
	c, _ := chunker.NewCDC(min, 5, max)
	chunks := splitAll(t, c, makeData(10000, 2))

	for i, ch := range chunks {
		if len(ch) > max {
			t.Errorf("chunk %d size %d exceeds max %d", i, len(ch), max)
		}
		if i < len(chunks)-1 && len(ch) < min {
			t.Errorf("non-final chunk %d size %d below min %d", i, len(ch), min)
		}
	}
}

// The same input must always produce the same boundaries — otherwise dedup
// across backups would be impossible.
func TestCDC_Deterministic(t *testing.T) {
	c, _ := chunker.NewCDC(16, 5, 256)
	data := makeData(5000, 3)
	a := splitAll(t, c, data)
	b := splitAll(t, c, data)

	if len(a) != len(b) {
		t.Fatalf("chunk counts differ between runs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			t.Fatalf("chunk %d differs between identical runs", i)
		}
	}
}

func TestCDC_InvalidParams(t *testing.T) {
	if _, err := chunker.NewCDC(0, 5, 256); err == nil {
		t.Error("expected error for min=0")
	}
	if _, err := chunker.NewCDC(300, 5, 256); err == nil {
		t.Error("expected error for min > max")
	}
}