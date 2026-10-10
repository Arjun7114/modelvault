// Package engine ties a Chunker and a Backend together to perform backups and
// restores. It is the core of modelvault: the CLI (and later a REST API) just
// call these methods.
package engine

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Arjun7114/modelvault/internal/backend"
	"github.com/Arjun7114/modelvault/internal/chunker"
	"github.com/Arjun7114/modelvault/internal/snapshot"
)

// Engine performs backups and restores using an injected chunker and backend.
type Engine struct {
	chunker chunker.Chunker
	backend backend.Backend
}

// New returns an Engine backed by the given chunker and backend.
func New(c chunker.Chunker, b backend.Backend) *Engine {
	return &Engine{chunker: c, backend: b}
}

// BackupStats summarizes a backup. The gap between TotalChunks and NewChunks
// is the de-duplication: chunks already present in the backend.
type BackupStats struct {
	TotalChunks int
	NewChunks   int
	DupChunks   int
	TotalBytes  int64
	StoredBytes int64
}

// storeChunk records one hashed chunk: it updates stats and the manifest, then
// stores the chunk only if the backend doesn't already have it (dedup). It is
// called serially (by Backup directly, or by BackupConcurrent's collector in
// chunk order), so it needs no locking.
func (e *Engine) storeChunk(snap *snapshot.Snapshot, stats *BackupStats, hash string, data []byte) error {
	stats.TotalChunks++
	stats.TotalBytes += int64(len(data))
	snap.Chunks = append(snap.Chunks, hash)

	has, err := e.backend.HasChunk(hash)
	if err != nil {
		return fmt.Errorf("checking chunk %s: %w", hash, err)
	}
	if has {
		stats.DupChunks++
		return nil
	}
	if err := e.backend.PutChunk(hash, data); err != nil {
		return fmt.Errorf("storing chunk %s: %w", hash, err)
	}
	stats.NewChunks++
	stats.StoredBytes += int64(len(data))
	return nil
}

// finalize sets the snapshot size, assigns it an id, serializes it, and stores it.
func (e *Engine) finalize(snap *snapshot.Snapshot, totalBytes int64) error {
	snap.Size = totalBytes
	id, err := newSnapshotID()
	if err != nil {
		return fmt.Errorf("generating snapshot id: %w", err)
	}
	snap.ID = id
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing snapshot: %w", err)
	}
	if err := e.backend.PutSnapshot(id, data); err != nil {
		return fmt.Errorf("storing snapshot: %w", err)
	}
	return nil
}

// Backup reads all of r, splits it into chunks, stores the chunks it hasn't
// seen before, and writes a snapshot manifest. This is the serial path.
func (e *Engine) Backup(source string, r io.Reader) (*snapshot.Snapshot, BackupStats, error) {
	var stats BackupStats
	snap := &snapshot.Snapshot{Source: source, CreatedAt: time.Now().UTC()}

	err := e.chunker.Split(r, func(chunk []byte) error {
		sum := sha256.Sum256(chunk)
		return e.storeChunk(snap, &stats, hex.EncodeToString(sum[:]), chunk)
	})
	if err != nil {
		return nil, stats, err
	}

	if err := e.finalize(snap, stats.TotalBytes); err != nil {
		return nil, stats, err
	}
	return snap, stats, nil
}

// BackupConcurrent is like Backup, but hashes chunks across `workers` goroutines
// (fan-out), then reassembles the results in order (fan-in) before storing them.
// Storing itself is still serial here; a bounded store pool comes in Phase 3.2.
func (e *Engine) BackupConcurrent(source string, r io.Reader, workers int) (*snapshot.Snapshot, BackupStats, error) {
	if workers < 1 {
		workers = 1
	}
	var stats BackupStats
	snap := &snapshot.Snapshot{Source: source, CreatedAt: time.Now().UTC()}

	type job struct {
		index int
		data  []byte
	}
	type result struct {
		index int
		hash  string
		data  []byte
	}

	jobs := make(chan job, workers)
	results := make(chan result, workers)

	// Producer: split into chunks and feed the hashers, tagging each with its
	// position so the collector can restore order later. The chunker hands us a
	// fresh slice per chunk, so passing data across goroutines is safe.
	var splitErr error
	go func() {
		i := 0
		splitErr = e.chunker.Split(r, func(chunk []byte) error {
			jobs <- job{index: i, data: chunk}
			i++
			return nil
		})
		close(jobs)
	}()

	// Fan-out: each worker hashes chunks in parallel (SHA-256 is CPU-bound).
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				sum := sha256.Sum256(j.data)
				results <- result{index: j.index, hash: hex.EncodeToString(sum[:]), data: j.data}
			}
		}()
	}

	// Fan-in: close results once every hasher has finished.
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collector: buffer out-of-order results in a map and emit them in index
	// order, so the manifest lists chunks exactly as they appeared in the file.
	pending := make(map[int]result)
	next := 0
	var storeErr error
	for res := range results {
		pending[res.index] = res
		for {
			cur, ok := pending[next]
			if !ok {
				break
			}
			delete(pending, next)
			next++
			if storeErr != nil {
				continue // already failed; keep draining so goroutines don't leak
			}
			if err := e.storeChunk(snap, &stats, cur.hash, cur.data); err != nil {
				storeErr = err
			}
		}
	}

	if splitErr != nil {
		return nil, stats, splitErr
	}
	if storeErr != nil {
		return nil, stats, storeErr
	}
	if err := e.finalize(snap, stats.TotalBytes); err != nil {
		return nil, stats, err
	}
	return snap, stats, nil
}

// newSnapshotID returns a sortable, unique id: a UTC timestamp plus a few
// random bytes, so two backups in the same second don't collide.
func newSnapshotID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	ts := time.Now().UTC().Format("20060102T150405Z")
	return fmt.Sprintf("%s-%s", ts, hex.EncodeToString(b[:])), nil
}

// RestoreStats summarizes a restore.
type RestoreStats struct {
	Chunks int
	Bytes  int64
}

// Restore reads the snapshot with the given id and writes its reconstructed
// contents to w, in chunk order. Every chunk is re-hashed and checked against
// its stored address before being written, so corruption is caught on read.
func (e *Engine) Restore(snapshotID string, w io.Writer) (RestoreStats, error) {
	var stats RestoreStats

	data, err := e.backend.GetSnapshot(snapshotID)
	if err != nil {
		return stats, fmt.Errorf("reading snapshot: %w", err)
	}
	var snap snapshot.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return stats, fmt.Errorf("parsing snapshot: %w", err)
	}

	for i, hash := range snap.Chunks {
		chunk, err := e.backend.GetChunk(hash)
		if err != nil {
			return stats, fmt.Errorf("reading chunk %d (%s): %w", i, hash, err)
		}
		sum := sha256.Sum256(chunk)
		if got := hex.EncodeToString(sum[:]); got != hash {
			return stats, fmt.Errorf("integrity error on chunk %d: want %s, got %s", i, hash, got)
		}
		if _, err := w.Write(chunk); err != nil {
			return stats, fmt.Errorf("writing chunk %d: %w", i, err)
		}
		stats.Chunks++
		stats.Bytes += int64(len(chunk))
	}

	if stats.Bytes != snap.Size {
		return stats, fmt.Errorf("size mismatch: snapshot says %d bytes, restored %d", snap.Size, stats.Bytes)
	}
	return stats, nil
}