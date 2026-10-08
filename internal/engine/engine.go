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

// Backup reads all of r, splits it into chunks, stores the chunks it hasn't
// seen before, and writes a snapshot manifest. source is a human-readable
// label (e.g. the original file path) recorded in the manifest.
func (e *Engine) Backup(source string, r io.Reader) (*snapshot.Snapshot, BackupStats, error) {
	var stats BackupStats
	snap := &snapshot.Snapshot{
		Source:    source,
		CreatedAt: time.Now().UTC(),
	}

	err := e.chunker.Split(r, func(chunk []byte) error {
		// The chunk's SHA-256, in hex, IS its address.
		sum := sha256.Sum256(chunk)
		hash := hex.EncodeToString(sum[:])

		stats.TotalChunks++
		stats.TotalBytes += int64(len(chunk))
		snap.Chunks = append(snap.Chunks, hash)

		has, err := e.backend.HasChunk(hash)
		if err != nil {
			return fmt.Errorf("checking chunk %s: %w", hash, err)
		}
		if has {
			stats.DupChunks++
			return nil // already stored: de-duplicated
		}
		if err := e.backend.PutChunk(hash, chunk); err != nil {
			return fmt.Errorf("storing chunk %s: %w", hash, err)
		}
		stats.NewChunks++
		stats.StoredBytes += int64(len(chunk))
		return nil
	})
	if err != nil {
		return nil, stats, err
	}

	snap.Size = stats.TotalBytes

	id, err := newSnapshotID()
	if err != nil {
		return nil, stats, fmt.Errorf("generating snapshot id: %w", err)
	}
	snap.ID = id

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return nil, stats, fmt.Errorf("serializing snapshot: %w", err)
	}
	if err := e.backend.PutSnapshot(id, data); err != nil {
		return nil, stats, fmt.Errorf("storing snapshot: %w", err)
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