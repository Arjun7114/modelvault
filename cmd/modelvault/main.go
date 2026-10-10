package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/Arjun7114/modelvault/internal/backend"
	"github.com/Arjun7114/modelvault/internal/chunker"
	"github.com/Arjun7114/modelvault/internal/engine"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	switch os.Args[1] {
	case "backup":
		if err := runBackup(ctx, os.Args[2:]); err != nil {
			fail(err)
		}
	case "list":
		if err := runList(ctx, os.Args[2:]); err != nil {
			fail(err)
		}
	case "restore":
		if err := runRestore(ctx, os.Args[2:]); err != nil {
			fail(err)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, "modelvault: content-addressed backup for ML model artifacts")
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  modelvault backup  [--backend local|s3] [--vault DIR] [--bucket NAME] [--region R] [--chunker fixed|cdc] [--chunk-size N] <file>")
	fmt.Fprintln(os.Stderr, "  modelvault list    [--backend local|s3] [--vault DIR] [--bucket NAME] [--region R]")
	fmt.Fprintln(os.Stderr, "  modelvault restore [--backend local|s3] [--vault DIR] [--bucket NAME] [--region R] --out FILE <snapshot-id>")
}

// buildBackend constructs the chosen storage backend. Adding Azure later is one
// more case here — the engine never changes.
func buildBackend(ctx context.Context, kind, vault, bucket, region string) (backend.Backend, error) {
	switch kind {
	case "local":
		return backend.NewLocal(vault)
	case "s3":
		return backend.NewS3(ctx, bucket, region)
	default:
		return nil, fmt.Errorf("unknown backend %q (want \"local\" or \"s3\")", kind)
	}
}

func buildChunker(kind string, chunkSize int) (chunker.Chunker, error) {
	switch kind {
	case "fixed":
		return chunker.NewFixed(chunkSize), nil
	case "cdc":
		avgBits := log2Floor(chunkSize)
		if avgBits < 1 {
			avgBits = 1
		}
		min := chunkSize / 4
		if min < 1 {
			min = 1
		}
		max := chunkSize * 4
		return chunker.NewCDC(min, avgBits, max)
	default:
		return nil, fmt.Errorf("unknown chunker %q (want \"fixed\" or \"cdc\")", kind)
	}
}

func log2Floor(n int) int {
	bits := 0
	for n > 1 {
		n >>= 1
		bits++
	}
	return bits
}

func runBackup(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	backendKind := fs.String("backend", "local", "storage backend: local or s3")
	vault := fs.String("vault", "vault", "path to the vault directory (local backend)")
	bucket := fs.String("bucket", "", "S3 bucket name (s3 backend)")
	region := fs.String("region", "", "S3 region (s3 backend; default from AWS config)")
	chunkerKind := fs.String("chunker", "fixed", "chunking strategy: fixed or cdc")
	chunkSize := fs.Int("chunk-size", 4096, "chunk size in bytes (fixed) / average target (cdc)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("expected exactly one file to back up, got %d", fs.NArg())
	}
	srcPath := fs.Arg(0)

	ck, err := buildChunker(*chunkerKind, *chunkSize)
	if err != nil {
		return err
	}

	f, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("opening source: %w", err)
	}
	defer f.Close()

	be, err := buildBackend(ctx, *backendKind, *vault, *bucket, *region)
	if err != nil {
		return err
	}
	eng := engine.New(ck, be)

	snap, stats, err := eng.Backup(ctx, filepath.Clean(srcPath), f)
	if err != nil {
		return err
	}

	fmt.Printf("backed up %q (backend: %s, chunker: %s)\n", srcPath, *backendKind, *chunkerKind)
	fmt.Printf("  snapshot:     %s\n", snap.ID)
	fmt.Printf("  total chunks: %d (%d bytes)\n", stats.TotalChunks, stats.TotalBytes)
	fmt.Printf("  new chunks:   %d (%d bytes stored)\n", stats.NewChunks, stats.StoredBytes)
	fmt.Printf("  deduplicated: %d chunks skipped\n", stats.DupChunks)
	return nil
}

func runList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	backendKind := fs.String("backend", "local", "storage backend: local or s3")
	vault := fs.String("vault", "vault", "path to the vault directory (local backend)")
	bucket := fs.String("bucket", "", "S3 bucket name (s3 backend)")
	region := fs.String("region", "", "S3 region (s3 backend)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	be, err := buildBackend(ctx, *backendKind, *vault, *bucket, *region)
	if err != nil {
		return err
	}
	ids, err := be.ListSnapshots(ctx)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Println("no snapshots")
		return nil
	}
	for _, id := range ids {
		fmt.Println(id)
	}
	return nil
}

func runRestore(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	backendKind := fs.String("backend", "local", "storage backend: local or s3")
	vault := fs.String("vault", "vault", "path to the vault directory (local backend)")
	bucket := fs.String("bucket", "", "S3 bucket name (s3 backend)")
	region := fs.String("region", "", "S3 region (s3 backend)")
	out := fs.String("out", "", "output file path (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("expected exactly one snapshot id, got %d", fs.NArg())
	}
	if *out == "" {
		return fmt.Errorf("--out is required")
	}
	snapshotID := fs.Arg(0)

	be, err := buildBackend(ctx, *backendKind, *vault, *bucket, *region)
	if err != nil {
		return err
	}
	eng := engine.New(nil, be)

	f, err := os.Create(*out)
	if err != nil {
		return fmt.Errorf("creating output: %w", err)
	}
	defer f.Close()

	stats, err := eng.Restore(ctx, snapshotID, f)
	if err != nil {
		return err
	}

	fmt.Printf("restored snapshot %s -> %q (backend: %s)\n", snapshotID, *out, *backendKind)
	fmt.Printf("  chunks: %d (%d bytes)\n", stats.Chunks, stats.Bytes)
	return nil
}