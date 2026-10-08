// Package snapshot defines the manifest that records a single point-in-time
// backup: which chunks, in what order, make up the backed-up data.
package snapshot

import "time"

// Snapshot is the manifest for one backup. Restoring means fetching each
// hash in Chunks (in order) from the backend and concatenating the bytes.
//
// It stores only chunk *hashes*, not data — the data lives once in the
// backend and is shared across every snapshot that references it. That
// sharing is exactly what makes incremental, de-duplicated backups work.
type Snapshot struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	Chunks    []string  `json:"chunks"`
	Size      int64     `json:"size"`
}