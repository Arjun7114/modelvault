// Package backend defines the storage contract for modelvault. A Backend is
// a content-addressed store: chunks are keyed by their own hash, so storing
// the same bytes twice is a no-op. Implementations: local disk (first),
// then S3 and Azure Blob behind this same interface.
package backend

import "context"

// Backend stores and retrieves content-addressed chunks and snapshot
// manifests. Every method takes a context.Context so cloud implementations
// (S3, Azure Blob) honor cancellation and deadlines; the local disk backend
// checks it too, for consistency.
type Backend interface {
	// PutChunk stores data under hash. It must be idempotent: storing a
	// hash that already exists is a successful no-op.
	PutChunk(ctx context.Context, hash string, data []byte) error
	// HasChunk reports whether a chunk with this hash already exists.
	HasChunk(ctx context.Context, hash string) (bool, error)
	// GetChunk returns the data previously stored under hash.
	GetChunk(ctx context.Context, hash string) ([]byte, error)

	// PutSnapshot stores a serialized snapshot manifest under id.
	PutSnapshot(ctx context.Context, id string, data []byte) error
	// GetSnapshot returns the serialized manifest stored under id.
	GetSnapshot(ctx context.Context, id string) ([]byte, error)
	// ListSnapshots returns the ids of all stored snapshots.
	ListSnapshots(ctx context.Context) ([]string, error)
}