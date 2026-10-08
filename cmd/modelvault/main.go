package main

import (
	"flag"
	"fmt"
	"os"
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
	switch os.Args[1] {
	case "backup":
		if err := runBackup(os.Args[2:]); err != nil {
			fail(err)
		}
	case "list":
		if err := runList(os.Args[2:]); err != nil {
			fail(err)
		}
	case "restore":
		if err := runRestore(os.Args[2:]); err != nil {
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
	fmt.Fprintln(os.Stderr, "  modelvault backup  [--vault DIR] [--chunk-size N] <file>")
	fmt.Fprintln(os.Stderr, "  modelvault list    [--vault DIR]")
	fmt.Fprintln(os.Stderr, "  modelvault restore [--vault DIR] --out FILE <snapshot-id>")
}

func runBackup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	vault := fs.String("vault", "vault", "path to the vault directory")
	chunkSize := fs.Int("chunk-size", 4096, "chunk size in bytes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("expected exactly one file to back up, got %d", fs.NArg())
	}
	srcPath := fs.Arg(0)

	f, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("opening source: %w", err)
	}
	defer f.Close()

	be, err := backend.NewLocal(*vault)
	if err != nil {
		return fmt.Errorf("opening vault: %w", err)
	}
	eng := engine.New(chunker.NewFixed(*chunkSize), be)

	snap, stats, err := eng.Backup(filepath.Clean(srcPath), f)
	if err != nil {
		return err
	}

	fmt.Printf("backed up %q\n", srcPath)
	fmt.Printf("  snapshot:     %s\n", snap.ID)
	fmt.Printf("  total chunks: %d (%d bytes)\n", stats.TotalChunks, stats.TotalBytes)
	fmt.Printf("  new chunks:   %d (%d bytes stored)\n", stats.NewChunks, stats.StoredBytes)
	fmt.Printf("  deduplicated: %d chunks skipped\n", stats.DupChunks)
	return nil
}

func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	vault := fs.String("vault", "vault", "path to the vault directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	be, err := backend.NewLocal(*vault)
	if err != nil {
		return fmt.Errorf("opening vault: %w", err)
	}
	ids, err := be.ListSnapshots()
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

func runRestore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	vault := fs.String("vault", "vault", "path to the vault directory")
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

	be, err := backend.NewLocal(*vault)
	if err != nil {
		return fmt.Errorf("opening vault: %w", err)
	}
	// Restore does not use a chunker, so we pass nil.
	eng := engine.New(nil, be)

	f, err := os.Create(*out)
	if err != nil {
		return fmt.Errorf("creating output: %w", err)
	}
	defer f.Close()

	stats, err := eng.Restore(snapshotID, f)
	if err != nil {
		return err
	}

	fmt.Printf("restored snapshot %s -> %q\n", snapshotID, *out)
	fmt.Printf("  chunks: %d (%d bytes)\n", stats.Chunks, stats.Bytes)
	return nil
}