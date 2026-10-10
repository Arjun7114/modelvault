package backend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LocalBackend stores chunks and snapshots as files on the local filesystem,
// under a single root directory:
//
//	<root>/chunks/<hash>        one file per unique chunk
//	<root>/snapshots/<id>.json  one manifest per backup
//
// It's the development backend: no network, trivial to inspect by hand. The
// S3 and Azure Blob backends implement this same interface.
type LocalBackend struct {
	root string
}

var _ Backend = (*LocalBackend)(nil)

// NewLocal creates the directory layout under root (if needed) and returns a
// ready-to-use LocalBackend.
func NewLocal(root string) (*LocalBackend, error) {
	for _, sub := range []string{"chunks", "snapshots"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return nil, fmt.Errorf("creating %s dir: %w", sub, err)
		}
	}
	return &LocalBackend{root: root}, nil
}

// safeKey rejects keys that could escape the intended directory.
func safeKey(key string) error {
	if key == "" || strings.ContainsAny(key, `/\`) || strings.Contains(key, "..") {
		return fmt.Errorf("backend: unsafe key %q", key)
	}
	return nil
}

func (b *LocalBackend) chunkPath(hash string) string {
	return filepath.Join(b.root, "chunks", hash)
}

func (b *LocalBackend) snapshotPath(id string) string {
	return filepath.Join(b.root, "snapshots", id+".json")
}

// writeFileAtomic writes to a temp file in the same directory, then renames it
// into place. The rename is atomic, so a reader never sees a half-written file.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (b *LocalBackend) PutChunk(ctx context.Context, hash string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := safeKey(hash); err != nil {
		return err
	}
	path := b.chunkPath(hash)
	if _, err := os.Stat(path); err == nil {
		return nil // already present: dedup
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeFileAtomic(path, data)
}

func (b *LocalBackend) HasChunk(ctx context.Context, hash string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := safeKey(hash); err != nil {
		return false, err
	}
	_, err := os.Stat(b.chunkPath(hash))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (b *LocalBackend) GetChunk(ctx context.Context, hash string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := safeKey(hash); err != nil {
		return nil, err
	}
	return os.ReadFile(b.chunkPath(hash))
}

func (b *LocalBackend) PutSnapshot(ctx context.Context, id string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := safeKey(id); err != nil {
		return err
	}
	return writeFileAtomic(b.snapshotPath(id), data)
}

func (b *LocalBackend) GetSnapshot(ctx context.Context, id string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := safeKey(id); err != nil {
		return nil, err
	}
	return os.ReadFile(b.snapshotPath(id))
}

func (b *LocalBackend) ListSnapshots(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(b.root, "snapshots"))
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".json"))
	}
	return ids, nil
}