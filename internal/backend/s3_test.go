package backend_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/Arjun7114/modelvault/internal/backend"
)

// TestS3Backend_Integration runs only when MODELVAULT_S3_BUCKET is set, so the
// normal test suite doesn't require AWS. It exercises the backend against a
// real S3 bucket.
func TestS3Backend_Integration(t *testing.T) {
	bucket := os.Getenv("MODELVAULT_S3_BUCKET")
	if bucket == "" {
		t.Skip("set MODELVAULT_S3_BUCKET to run the S3 integration test")
	}
	region := os.Getenv("MODELVAULT_S3_REGION")
	if region == "" {
		region = "ap-south-1"
	}
	ctx := context.Background()

	b, err := backend.NewS3(ctx, bucket, region)
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}

	hash := "testchunk0123456789abcdef"
	data := []byte("hello from modelvault s3 test")

	if err := b.PutChunk(ctx, hash, data); err != nil {
		t.Fatalf("PutChunk: %v", err)
	}
	if ok, err := b.HasChunk(ctx, hash); err != nil || !ok {
		t.Fatalf("HasChunk = (%v, %v), want (true, nil)", ok, err)
	}
	got, err := b.GetChunk(ctx, hash)
	if err != nil {
		t.Fatalf("GetChunk: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("GetChunk = %q, want %q", got, data)
	}

	// A missing chunk must report false, not an error.
	if ok, err := b.HasChunk(ctx, "definitelymissingchunk999"); err != nil {
		t.Fatalf("HasChunk(missing): %v", err)
	} else if ok {
		t.Error("HasChunk(missing) = true, want false")
	}

	id := "testsnap0123456789"
	if err := b.PutSnapshot(ctx, id, []byte(`{"id":"testsnap"}`)); err != nil {
		t.Fatalf("PutSnapshot: %v", err)
	}
	snapData, err := b.GetSnapshot(ctx, id)
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if string(snapData) != `{"id":"testsnap"}` {
		t.Errorf("GetSnapshot = %q", snapData)
	}

	ids, err := b.ListSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	found := false
	for _, x := range ids {
		if x == id {
			found = true
		}
	}
	if !found {
		t.Errorf("ListSnapshots did not contain %q; got %v", id, ids)
	}
}