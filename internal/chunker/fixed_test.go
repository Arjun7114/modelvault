package chunker_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Arjun7114/modelvault/internal/chunker"
)

func TestFixedChunker_SplitsUnevenly(t *testing.T) {
	// 10 bytes at size 4 -> chunks of 4, 4, 2.
	c := chunker.NewFixed(4)

	var got [][]byte
	err := c.Split(strings.NewReader("abcdefghij"), func(chunk []byte) error {
		got = append(got, chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("Split returned error: %v", err)
	}

	want := [][]byte{[]byte("abcd"), []byte("efgh"), []byte("ij")}
	if len(got) != len(want) {
		t.Fatalf("got %d chunks, want %d", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Errorf("chunk %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFixedChunker_EmptyInput(t *testing.T) {
	c := chunker.NewFixed(4)
	count := 0
	err := c.Split(strings.NewReader(""), func(chunk []byte) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("Split returned error: %v", err)
	}
	if count != 0 {
		t.Errorf("empty input produced %d chunks, want 0", count)
	}
}

func TestFixedChunker_InvalidSize(t *testing.T) {
	c := chunker.NewFixed(0)
	err := c.Split(strings.NewReader("abc"), func(chunk []byte) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error for non-positive size, got nil")
	}
}