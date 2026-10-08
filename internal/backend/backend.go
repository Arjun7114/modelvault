// Package backend defines the storage contract for modelvault. A Backend is
// a content-addressed store: chunks are keyed by their own hash, so storing
// the same bytes twice is a no-op. Implementations: local disk (first),
// then S3 and Azure Blob behind this same interface.
package backend

// Backend stores and retrieves content-addressed chunks and snapshot
// manifests. Every method returns an error so remote implementations
// (with network failures) fit the same contract as the local one.
type Backend interface {
	// PutChunk stores data under hash. It must be idempotent: storing a
	// hash that already exists is a successful no-op.
	PutChunk(hash string, data []byte) error
	// HasChunk reports whether a chunk with this hash already exists.
	// The engine uses it to skip re-storing duplicate chunks.
	HasChunk(hash string) (bool, error)
	// GetChunk returns the data previously stored under hash.
	GetChunk(hash string) ([]byte, error)

	// PutSnapshot stores a serialized snapshot manifest under id.
	PutSnapshot(id string, data []byte) error
	// GetSnapshot returns the serialized manifest stored under id.
	GetSnapshot(id string) ([]byte, error)
	// ListSnapshots returns the ids of all stored snapshots.
	ListSnapshots() ([]string, error)
}