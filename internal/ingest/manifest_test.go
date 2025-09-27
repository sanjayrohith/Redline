package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testServer(t *testing.T, sha string, tree string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/revision/"):
			_, _ = w.Write([]byte(`{"sha":"` + sha + `"}`))
		case strings.Contains(r.URL.Path, "/tree/"):
			_, _ = w.Write([]byte(tree))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestFetchManifest_Success(t *testing.T) {
	tree := `[
		{"type":"file","path":"config.json","size":712,"oid":"abc123"},
		{"type":"file","path":"model.safetensors","size":16000000000,"oid":"def456"},
		{"type":"directory","path":"checkpoints"}
	]`
	server := testServer(t, "deadbeef", tree)
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	ref := Reference{Owner: "org", Name: "model", Revision: "main"}

	manifest, err := client.FetchManifest(context.Background(), ref)
	if err != nil {
		t.Fatalf("FetchManifest() error = %v", err)
	}

	if manifest.RevisionSHA != "deadbeef" {
		t.Errorf("RevisionSHA = %q, want deadbeef", manifest.RevisionSHA)
	}
	if len(manifest.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2 (directories excluded)", len(manifest.Files))
	}
	if manifest.Files[1].Path != "model.safetensors" || manifest.Files[1].Size != 16000000000 {
		t.Errorf("Files[1] = %+v", manifest.Files[1])
	}
}

func TestFetchManifest_RepositoryNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	ref := Reference{Owner: "org", Name: "missing", Revision: "main"}

	if _, err := client.FetchManifest(context.Background(), ref); err != ErrRepositoryNotFound {
		t.Errorf("FetchManifest() error = %v, want ErrRepositoryNotFound", err)
	}
}

func TestFetchManifest_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/revision/") {
			_, _ = w.Write([]byte(`{"sha":"deadbeef"}`))
			return
		}
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	ref := Reference{Owner: "org", Name: "model", Revision: "main"}

	if _, err := client.FetchManifest(context.Background(), ref); err == nil {
		t.Fatal("FetchManifest() error = nil, want error for malformed JSON")
	}
}

func TestFetchManifest_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	ref := Reference{Owner: "org", Name: "model", Revision: "main"}

	if _, err := client.FetchManifest(context.Background(), ref); err == nil {
		t.Fatal("FetchManifest() error = nil, want error for 500")
	}
}
