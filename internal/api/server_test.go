package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Arjun7114/modelvault/internal/api"
	"github.com/Arjun7114/modelvault/internal/backend"
	"github.com/Arjun7114/modelvault/internal/chunker"
	"github.com/Arjun7114/modelvault/internal/engine"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	be, err := backend.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(chunker.NewFixed(1024), be)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil)) // silent in tests
	return httptest.NewServer(api.NewServer(eng, logger).Routes())
}

func TestAPI_BackupListRestore(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	original := []byte("hello modelvault over http, this is a test payload")

	resp, err := http.Post(ts.URL+"/v1/backup?source=test", "application/octet-stream", bytes.NewReader(original))
	if err != nil {
		t.Fatalf("POST backup: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("backup status = %d, want 201", resp.StatusCode)
	}
	var backupResp struct {
		Snapshot struct {
			ID string `json:"id"`
		} `json:"snapshot"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&backupResp); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	id := backupResp.Snapshot.ID
	if id == "" {
		t.Fatal("empty snapshot id")
	}

	resp, err = http.Get(ts.URL + "/v1/snapshots")
	if err != nil {
		t.Fatal(err)
	}
	var listResp struct {
		Snapshots []string `json:"snapshots"`
	}
	json.NewDecoder(resp.Body).Decode(&listResp)
	resp.Body.Close()
	found := false
	for _, x := range listResp.Snapshots {
		if x == id {
			found = true
		}
	}
	if !found {
		t.Errorf("snapshot %s not in list %v", id, listResp.Snapshots)
	}

	resp, err = http.Get(ts.URL + "/v1/restore/" + id)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(got, original) {
		t.Errorf("restore mismatch: got %q, want %q", got, original)
	}
}

func TestAPI_Health(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want 200", resp.StatusCode)
	}
}