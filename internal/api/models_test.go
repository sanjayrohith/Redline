package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/db"
)

type fakeModelLister struct {
	models []db.Model
	err    error
}

func (f *fakeModelLister) List(context.Context) ([]db.Model, error) {
	return f.models, f.err
}

func TestModelsHandler_ListsModels(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	lister := &fakeModelLister{models: []db.Model{
		{ID: "1", RepoURL: "hf://org/model-a", Revision: "main", CreatedAt: created},
		{ID: "2", RepoURL: "hf://org/model-b", Revision: "main", CreatedAt: created},
	}}

	rec := httptest.NewRecorder()
	ModelsHandler(lister).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp ModelListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Object != "list" {
		t.Errorf("Object = %q, want list", resp.Object)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("len(Data) = %d, want 2", len(resp.Data))
	}
	if resp.Data[0].ID != "hf://org/model-a" || resp.Data[0].Object != "model" {
		t.Errorf("Data[0] = %+v", resp.Data[0])
	}
}

func TestModelsHandler_EmptyCatalog(t *testing.T) {
	lister := &fakeModelLister{models: nil}

	rec := httptest.NewRecorder()
	ModelsHandler(lister).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp ModelListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Errorf("len(Data) = %d, want 0", len(resp.Data))
	}
}

func TestModelsHandler_StorageError(t *testing.T) {
	lister := &fakeModelLister{err: errors.New("connection refused")}

	rec := httptest.NewRecorder()
	ModelsHandler(lister).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

type fakeModelGetter struct {
	byID map[string]*db.Model
}

func (f *fakeModelGetter) GetByID(_ context.Context, id string) (*db.Model, error) {
	m, ok := f.byID[id]
	if !ok {
		return nil, db.ErrNotFound
	}
	return m, nil
}

func TestModelDetailHandler_ReturnsFullRecord(t *testing.T) {
	getter := &fakeModelGetter{byID: map[string]*db.Model{
		"model-1": {
			ID: "model-1", RepoURL: "org/model", Revision: "main", RevisionSHA: "sha1",
			Architecture: "llama", ParameterCount: 7_000_000_000, Dtype: "F16",
			VRAMEstimateFP16Bytes: 14_000_000_000, VRAMEstimateFP8Bytes: 7_000_000_000,
			VRAMEstimateInt4Bytes: 3_500_000_000, KVCacheBytes: 1_000_000_000,
			License: "apache-2.0", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}}
	mux := http.NewServeMux()
	mux.Handle("GET /v1/models/{id}", ModelDetailHandler(getter))

	req := httptest.NewRequest(http.MethodGet, "/v1/models/model-1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	var view ModelDetailView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if view.Architecture != "llama" || view.License != "apache-2.0" {
		t.Errorf("view = %+v", view)
	}
	if view.VRAMEstimateFP16Bytes != 14_000_000_000 {
		t.Errorf("VRAMEstimateFP16Bytes = %d, want 14000000000", view.VRAMEstimateFP16Bytes)
	}
}

func TestModelDetailHandler_UnknownIDIsNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /v1/models/{id}", ModelDetailHandler(&fakeModelGetter{byID: map[string]*db.Model{}}))

	req := httptest.NewRequest(http.MethodGet, "/v1/models/nope", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
