package backend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// S3Backend stores chunks and snapshots as objects in an S3 bucket:
//
//	chunks/<hash>
//	snapshots/<id>.json
//
// It implements the same Backend interface as LocalBackend, so the engine is
// unchanged — this is the payoff of the interface-driven design.
type S3Backend struct {
	client *s3.Client
	bucket string
}

var _ Backend = (*S3Backend)(nil)

// NewS3 builds an S3Backend for the given bucket, using the default AWS
// credential chain (env vars, ~/.aws/credentials, IAM role, ...). If region is
// empty, the region is resolved from the environment/config like the AWS CLI.
func NewS3(ctx context.Context, bucket, region string) (*S3Backend, error) {
	if bucket == "" {
		return nil, errors.New("backend: S3 bucket name required")
	}
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}
	return &S3Backend{client: s3.NewFromConfig(cfg), bucket: bucket}, nil
}

func chunkKey(hash string) string  { return "chunks/" + hash }
func snapshotKey(id string) string { return "snapshots/" + id + ".json" }

func (b *S3Backend) PutChunk(ctx context.Context, hash string, data []byte) error {
	if err := safeKey(hash); err != nil {
		return err
	}
	return b.put(ctx, chunkKey(hash), data)
}

func (b *S3Backend) HasChunk(ctx context.Context, hash string) (bool, error) {
	if err := safeKey(hash); err != nil {
		return false, err
	}
	return b.exists(ctx, chunkKey(hash))
}

func (b *S3Backend) GetChunk(ctx context.Context, hash string) ([]byte, error) {
	if err := safeKey(hash); err != nil {
		return nil, err
	}
	return b.get(ctx, chunkKey(hash))
}

func (b *S3Backend) PutSnapshot(ctx context.Context, id string, data []byte) error {
	if err := safeKey(id); err != nil {
		return err
	}
	return b.put(ctx, snapshotKey(id), data)
}

func (b *S3Backend) GetSnapshot(ctx context.Context, id string) ([]byte, error) {
	if err := safeKey(id); err != nil {
		return nil, err
	}
	return b.get(ctx, snapshotKey(id))
}

func (b *S3Backend) ListSnapshots(ctx context.Context) ([]string, error) {
	const prefix = "snapshots/"
	var ids []string
	p := s3.NewListObjectsV2Paginator(b.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(b.bucket),
		Prefix: aws.String(prefix),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing snapshots: %w", err)
		}
		for _, obj := range page.Contents {
			name := strings.TrimPrefix(aws.ToString(obj.Key), prefix)
			if strings.HasSuffix(name, ".json") {
				ids = append(ids, strings.TrimSuffix(name, ".json"))
			}
		}
	}
	return ids, nil
}

func (b *S3Backend) put(ctx context.Context, key string, data []byte) error {
	_, err := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("putting %s: %w", key, err)
	}
	return nil
}

func (b *S3Backend) get(ctx context.Context, key string) ([]byte, error) {
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("getting %s: %w", key, err)
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// exists uses HeadObject and treats a 404-class APIError as "not found" rather
// than a real error — the verified pattern for aws-sdk-go-v2.
func (b *S3Backend) exists(ctx context.Context, key string) (bool, error) {
	_, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	})
	if err == nil {
		return true, nil
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchKey":
			return false, nil
		}
	}
	return false, fmt.Errorf("heading %s: %w", key, err)
}