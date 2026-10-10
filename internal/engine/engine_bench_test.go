package engine_test

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/Arjun7114/modelvault/internal/backend"
	"github.com/Arjun7114/modelvault/internal/chunker"
	"github.com/Arjun7114/modelvault/internal/engine"
)

// memBackend is an in-memory, thread-safe Backend used to isolate CPU cost
// (hashing) from disk I/O in benchmarks. That it's this small to implement is
// itself a sign the Backend interface is well-factored.
type memBackend struct {
	mu     sync.Mutex
	chunks map[string][]byte
	snaps  map[string][]byte
}

var _ backend.Backend = (*memBackend)(nil)

func newMemBackend() *memBackend {
	return &memBackend{chunks: map[string][]byte{}, snaps: map[string][]byte{}}
}

func (m *memBackend) PutChunk(hash string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.chunks[hash]; !ok {
		cp := make([]byte, len(data))
		copy(cp, data)
		m.chunks[hash] = cp
	}
	return nil
}

func (m *memBackend) HasChunk(hash string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.chunks[hash]
	return ok, nil
}

func (m *memBackend) GetChunk(hash string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.chunks[hash]
	if !ok {
		return nil, fmt.Errorf("chunk %s not found", hash)
	}
	cp := make([]byte, len(d))
	copy(cp, d)
	return cp, nil
}

func (m *memBackend) PutSnapshot(id string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snaps[id] = data
	return nil
}

func (m *memBackend) GetSnapshot(id string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.snaps[id]
	if !ok {
		return nil, fmt.Errorf("snapshot %s not found", id)
	}
	return d, nil
}

func (m *memBackend) ListSnapshots() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.snaps))
	for id := range m.snaps {
		ids = append(ids, id)
	}
	return ids, nil
}

func benchmarkBackup(b *testing.B, concurrent bool, workers int) {
	data := makeBytes(8<<20, 123) // 8 MiB of unique data
	b.SetBytes(int64(len(data)))  // lets Go report MB/s
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		be := newMemBackend() // fresh each iteration so nothing dedups away
		eng := engine.New(chunker.NewFixed(4096), be)

		var err error
		if concurrent {
			_, _, err = eng.BackupConcurrent(context.Background(), "x", bytes.NewReader(data), workers)
		} else {
			_, _, err = eng.Backup("x", bytes.NewReader(data))
		}
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBackupSerial(b *testing.B) {
	benchmarkBackup(b, false, 0)
}

func BenchmarkBackupConcurrent(b *testing.B) {
	benchmarkBackup(b, true, runtime.NumCPU())
}