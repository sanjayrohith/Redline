package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sanjayrohith/redline/internal/db"
)

type fakeIngestionJobs struct {
	created   *db.IngestionJob
	byID      map[string]*db.IngestionJob
	createErr error
}

func (f *fakeIngestionJobs) Create(_ context.Context, repoURL, revision string) (*db.IngestionJob, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	job := &db.IngestionJob{ID: "job-1", RepoURL: repoURL, Revision: revision, State: db.IngestionStateQueued}
	f.created = job
	return job, nil
}

func (f *fakeIngestionJobs) GetByID(_ context.Context, id string) (*db.IngestionJob, error) {
	job, ok := f.byID[id]
	if !ok {
		return nil, db.ErrNotFound
	}
	return job, nil
}

type fakeDispatcher struct {
	enqueued []string
}

func (f *fakeDispatcher) Enqueue(_ context.Context, id, _ string) error {
	f.enqueued = append(f.enqueued, id)
	return nil
}

func TestCreateIngestionHandler_CreatesAndDispatches(t *testing.T) {
	jobs := &fakeIngestionJobs{}
	dispatcher := &fakeDispatcher{}
	handler := CreateIngestionHandler(jobs, dispatcher)

	req := httptest.NewRequest(http.MethodPost, "/v1/ingestions", strings.NewReader(`{"repo_url":"org/model"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body = %s", rec.Code, rec.Body.String())
	}
	var view IngestionJobView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if view.RepoURL != "org/model" || view.Revision != "main" {
		t.Errorf("view = %+v, want repo_url=org/model revision=main (defaulted)", view)
	}
	if len(dispatcher.enqueued) != 1 || dispatcher.enqueued[0] != "job-1" {
		t.Errorf("enqueued = %v, want [job-1]", dispatcher.enqueued)
	}
}

func TestCreateIngestionHandler_RejectsEmptyRepoURL(t *testing.T) {
	handler := CreateIngestionHandler(&fakeIngestionJobs{}, &fakeDispatcher{})
	req := httptest.NewRequest(http.MethodPost, "/v1/ingestions", strings.NewReader(`{"repo_url":""}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGetIngestionHandler_ReturnsCurrentProgress(t *testing.T) {
	errMsg := "checksum mismatch"
	jobs := &fakeIngestionJobs{byID: map[string]*db.IngestionJob{
		"job-1": {
			ID: "job-1", RepoURL: "org/model", Revision: "main",
			State: db.IngestionStateFailed, BytesTotal: 100, BytesDownloaded: 40,
			ErrorMessage: &errMsg,
		},
	}}
	mux := http.NewServeMux()
	mux.Handle("GET /v1/ingestions/{id}", GetIngestionHandler(jobs))

	req := httptest.NewRequest(http.MethodGet, "/v1/ingestions/job-1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	var view IngestionJobView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if view.State != db.IngestionStateFailed || view.ErrorMessage != "checksum mismatch" {
		t.Errorf("view = %+v", view)
	}
}

func TestGetIngestionHandler_UnknownIDIsNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /v1/ingestions/{id}", GetIngestionHandler(&fakeIngestionJobs{byID: map[string]*db.IngestionJob{}}))

	req := httptest.NewRequest(http.MethodGet, "/v1/ingestions/nope", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
