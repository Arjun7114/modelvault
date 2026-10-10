// Package engine ties a Chunker and a Backend together to perform backups and
// restores. It is the core of modelvault: the CLI (and later a REST API) just
// call these methods.
package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

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

// storeChunk records one hashed chunk serially (used by the serial Backup):
// updates stats and the manifest, then stores the chunk only if new.
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

// BackupConcurrent runs a three-stage pipeline coordinated by an errgroup:
//
//	producer -> [hash workers] -> collector -> [store workers]
//
// If any stage returns an error, or ctx is cancelled, the shared context is
// cancelled and every stage stops promptly; Wait returns the first error.
// Hashing is fanned out (CPU-bound); storing uses a bounded pool (I/O-bound);
// the collector reorders by index and dispatches each unique chunk once, so
// stats come out identical to the serial Backup.
func (e *Engine) BackupConcurrent(ctx context.Context, source string, r io.Reader, workers int) (*snapshot.Snapshot, BackupStats, error) {
	if workers < 1 {
		workers = 1
	}
	var stats BackupStats
	snap := &snapshot.Snapshot{Source: source, CreatedAt: time.Now().UTC()}

	type job struct {
		index int
		data  []byte
	}
	type hashed struct {
		index int
		hash  string
		data  []byte
	}
	type storeJob struct {
		hash string
		data []byte
	}

	jobs := make(chan job, workers)
	results := make(chan hashed, workers)
	storeJobs := make(chan storeJob, workers)

	g, ctx := errgroup.WithContext(ctx)

	// Producer: split into ordered, indexed chunks, honoring cancellation.
	g.Go(func() error {
		defer close(jobs)
		i := 0
		return e.chunker.Split(r, func(chunk []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			select {
			case jobs <- job{index: i, data: chunk}:
				i++
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	})

	// Fan-out hashers (CPU-bound). A WaitGroup lets us close results once all
	// hashers have finished — errgroup handles errors, not channel lifecycle.
	var wgHash sync.WaitGroup
	for w := 0; w < workers; w++ {
		wgHash.Add(1)
		g.Go(func() error {
			defer wgHash.Done()
			for j := range jobs {
				sum := sha256.Sum256(j.data)
				select {
				case results <- hashed{index: j.index, hash: hex.EncodeToString(sum[:]), data: j.data}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		})
	}
	go func() {
		wgHash.Wait()
		close(results)
	}()

	// Collector: reorder by index, build the manifest, dispatch unique chunks.
	var newChunks, dupCross, storedBytes int64
	var dupWithin int
	g.Go(func() error {
		defer close(storeJobs)
		pending := make(map[int]hashed)
		seen := make(map[string]bool)
		next := 0
		for res := range results {
			pending[res.index] = res
			for {
				cur, ok := pending[next]
				if !ok {
					break
				}
				delete(pending, next)
				next++

				stats.TotalChunks++
				stats.TotalBytes += int64(len(cur.data))
				snap.Chunks = append(snap.Chunks, cur.hash)

				if seen[cur.hash] {
					dupWithin++
					continue
				}
				seen[cur.hash] = true
				select {
				case storeJobs <- storeJob{hash: cur.hash, data: cur.data}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		return nil
	})

	// Bounded store pool (I/O-bound). Each unique hash is dispatched once, so
	// workers never race on the same hash; counters are atomic.
	for w := 0; w < workers; w++ {
		g.Go(func() error {
			for sj := range storeJobs {
				has, err := e.backend.HasChunk(sj.hash)
				if err != nil {
					return fmt.Errorf("checking chunk %s: %w", sj.hash, err)
				}
				if has {
					atomic.AddInt64(&dupCross, 1)
					continue
				}
				if err := e.backend.PutChunk(sj.hash, sj.data); err != nil {
					return fmt.Errorf("storing chunk %s: %w", sj.hash, err)
				}
				atomic.AddInt64(&newChunks, 1)
				atomic.AddInt64(&storedBytes, int64(len(sj.data)))
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, stats, err
	}

	stats.NewChunks = int(newChunks)
	stats.DupChunks = dupWithin + int(dupCross)
	stats.StoredBytes = storedBytes

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
// RestoreConcurrent is like Restore, but fetches and verifies chunks across
// `workers` goroutines (I/O-bound), then writes them to w in index order via a
// single collector — so the output is identical to Restore. Cancellation and
// errors propagate through the errgroup, same as BackupConcurrent.
func (e *Engine) RestoreConcurrent(ctx context.Context, snapshotID string, w io.Writer, workers int) (RestoreStats, error) {
	if workers < 1 {
		workers = 1
	}
	var stats RestoreStats

	data, err := e.backend.GetSnapshot(snapshotID)
	if err != nil {
		return stats, fmt.Errorf("reading snapshot: %w", err)
	}
	var snap snapshot.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return stats, fmt.Errorf("parsing snapshot: %w", err)
	}

	type job struct {
		index int
		hash  string
	}
	type fetched struct {
		index int
		data  []byte
	}

	jobs := make(chan job, workers)
	results := make(chan fetched, workers)

	g, ctx := errgroup.WithContext(ctx)

	// Producer: hand out chunk references in order.
	g.Go(func() error {
		defer close(jobs)
		for i, h := range snap.Chunks {
			if err := ctx.Err(); err != nil {
				return err
			}
			select {
			case jobs <- job{index: i, hash: h}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})

	// Fan-out: fetch each chunk and verify its hash in parallel.
	var wgFetch sync.WaitGroup
	for wk := 0; wk < workers; wk++ {
		wgFetch.Add(1)
		g.Go(func() error {
			defer wgFetch.Done()
			for j := range jobs {
				chunk, err := e.backend.GetChunk(j.hash)
				if err != nil {
					return fmt.Errorf("reading chunk %d (%s): %w", j.index, j.hash, err)
				}
				sum := sha256.Sum256(chunk)
				if got := hex.EncodeToString(sum[:]); got != j.hash {
					return fmt.Errorf("integrity error on chunk %d: want %s, got %s", j.index, j.hash, got)
				}
				select {
				case results <- fetched{index: j.index, data: chunk}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		})
	}
	go func() {
		wgFetch.Wait()
		close(results)
	}()

	// Collector: write chunks to w strictly in index order.
	g.Go(func() error {
		pending := make(map[int][]byte)
		next := 0
		for res := range results {
			pending[res.index] = res.data
			for {
				chunk, ok := pending[next]
				if !ok {
					break
				}
				delete(pending, next)
				if _, err := w.Write(chunk); err != nil {
					return fmt.Errorf("writing chunk %d: %w", next, err)
				}
				stats.Chunks++
				stats.Bytes += int64(len(chunk))
				next++
			}
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return stats, err
	}
	if stats.Bytes != snap.Size {
		return stats, fmt.Errorf("size mismatch: snapshot says %d bytes, restored %d", snap.Size, stats.Bytes)
	}
	return stats, nil
}