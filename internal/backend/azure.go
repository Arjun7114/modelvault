package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

// AzureBackend stores chunks and snapshots as blobs in an Azure Blob Storage
// container, under the same layout as the other backends:
//
//	chunks/<hash>
//	snapshots/<id>.json
//
// It implements the same Backend interface, so the engine is unchanged.
type AzureBackend struct {
	client    *azblob.Client
	container string
}

var _ Backend = (*AzureBackend)(nil)

// NewAzure builds an AzureBackend from a connection string (typically from the
// AZURE_STORAGE_CONNECTION_STRING environment variable) and a container name.
func NewAzure(connectionString, container string) (*AzureBackend, error) {
	if connectionString == "" {
		return nil, errors.New("backend: Azure connection string required")
	}
	if container == "" {
		return nil, errors.New("backend: Azure container name required")
	}
	client, err := azblob.NewClientFromConnectionString(connectionString, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure client: %w", err)
	}
	return &AzureBackend{client: client, container: container}, nil
}

func (b *AzureBackend) PutChunk(ctx context.Context, hash string, data []byte) error {
	if err := safeKey(hash); err != nil {
		return err
	}
	return b.put(ctx, chunkKey(hash), data)
}

func (b *AzureBackend) HasChunk(ctx context.Context, hash string) (bool, error) {
	if err := safeKey(hash); err != nil {
		return false, err
	}
	return b.exists(ctx, chunkKey(hash))
}

func (b *AzureBackend) GetChunk(ctx context.Context, hash string) ([]byte, error) {
	if err := safeKey(hash); err != nil {
		return nil, err
	}
	return b.get(ctx, chunkKey(hash))
}

func (b *AzureBackend) PutSnapshot(ctx context.Context, id string, data []byte) error {
	if err := safeKey(id); err != nil {
		return err
	}
	return b.put(ctx, snapshotKey(id), data)
}

func (b *AzureBackend) GetSnapshot(ctx context.Context, id string) ([]byte, error) {
	if err := safeKey(id); err != nil {
		return nil, err
	}
	return b.get(ctx, snapshotKey(id))
}

func (b *AzureBackend) ListSnapshots(ctx context.Context) ([]string, error) {
	const prefix = "snapshots/"
	p := prefix
	var ids []string
	pager := b.client.NewListBlobsFlatPager(b.container, &azblob.ListBlobsFlatOptions{Prefix: &p})
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing snapshots: %w", err)
		}
		for _, item := range page.Segment.BlobItems {
			if item == nil || item.Name == nil {
				continue
			}
			name := strings.TrimPrefix(*item.Name, prefix)
			if strings.HasSuffix(name, ".json") {
				ids = append(ids, strings.TrimSuffix(name, ".json"))
			}
		}
	}
	return ids, nil
}

func (b *AzureBackend) put(ctx context.Context, key string, data []byte) error {
	if _, err := b.client.UploadBuffer(ctx, b.container, key, data, nil); err != nil {
		return fmt.Errorf("uploading %s: %w", key, err)
	}
	return nil
}

func (b *AzureBackend) get(ctx context.Context, key string) ([]byte, error) {
	resp, err := b.client.DownloadStream(ctx, b.container, key, nil)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", key, err)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// exists checks a single blob via GetProperties, treating a 404 as "not found".
func (b *AzureBackend) exists(ctx context.Context, key string) (bool, error) {
	blobClient := b.client.ServiceClient().NewContainerClient(b.container).NewBlobClient(key)
	if _, err := blobClient.GetProperties(ctx, nil); err == nil {
		return true, nil
	} else {
		var respErr *azcore.ResponseError
		if errors.As(err, &respErr) && respErr.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, fmt.Errorf("checking %s: %w", key, err)
	}
}