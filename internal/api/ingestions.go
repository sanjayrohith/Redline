package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sanjayrohith/redline/internal/apierror"
	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/httpmw"
)

// IngestionJobCreator is the persistence dependency CreateIngestionHandler
// needs. It is satisfied by *db.IngestionJobRepository.
type IngestionJobCreator interface {
	Create(ctx context.Context, repoURL, revision string) (*db.IngestionJob, error)
}

// IngestionJobGetter is the persistence dependency GetIngestionHandler
// needs. It is satisfied by *db.IngestionJobRepository.
type IngestionJobGetter interface {
	GetByID(ctx context.Context, id string) (*db.IngestionJob, error)
}

// IngestionDispatcher hands a freshly created job off for asynchronous
// execution. It is satisfied by *queue.Queue's Enqueue method bound to the
// ingestion queue's name.
type IngestionDispatcher interface {
	Enqueue(ctx context.Context, id, payload string) error
}

// IngestionJobView is one ingestion job as returned to a client, tracking
// the same parse/download/verify/cache lifecycle the job state stream
// reports.
type IngestionJobView struct {
	ID              string `json:"id"`
	RepoURL         string `json:"repo_url"`
	Revision        string `json:"revision"`
	State           string `json:"state"`
	BytesTotal      int64  `json:"bytes_total"`
	BytesDownloaded int64  `json:"bytes_downloaded"`
	ErrorMessage    string `json:"error_message,omitempty"`
	ModelID         string `json:"model_id,omitempty"`
}

func ingestionJobView(j *db.IngestionJob) IngestionJobView {
	v := IngestionJobView{
		ID: j.ID, RepoURL: j.RepoURL, Revision: j.Revision, State: j.State,
		BytesTotal: j.BytesTotal, BytesDownloaded: j.BytesDownloaded,
	}
	if j.ErrorMessage != nil {
		v.ErrorMessage = *j.ErrorMessage
	}
	if j.ModelID != nil {
		v.ModelID = *j.ModelID
	}
	return v
}

type createIngestionRequest struct {
	RepoURL  string `json:"repo_url"`
	Revision string `json:"revision"`
}

// CreateIngestionHandler implements POST /v1/ingestions: queues a new
// ingestion job for the given repository reference and dispatches it for
// asynchronous execution, returning immediately with the job's id so the
// client can begin polling its live progress.
func CreateIngestionHandler(jobs IngestionJobCreator, dispatcher IngestionDispatcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())

		var req createIngestionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apierror.Write(w, http.StatusBadRequest, apierror.CodeValidation, "malformed request body", requestID)
			return
		}
		req.RepoURL = strings.TrimSpace(req.RepoURL)
		if req.RepoURL == "" {
			apierror.Write(w, http.StatusBadRequest, apierror.CodeValidation, "repo_url is required", requestID)
			return
		}
		if req.Revision == "" {
			req.Revision = "main"
		}

		job, err := jobs.Create(r.Context(), req.RepoURL, req.Revision)
		if err != nil {
			apierror.WriteStorageError(w, requestID, err)
			return
		}

		if err := dispatcher.Enqueue(r.Context(), job.ID, job.ID); err != nil {
			apierror.Write(w, http.StatusInternalServerError, apierror.CodeInternal, "failed to dispatch ingestion job", requestID)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(ingestionJobView(job))
	}
}

// GetIngestionHandler implements GET /v1/ingestions/{id}: the job's
// current state, polled by the frontend to render live progress.
func GetIngestionHandler(jobs IngestionJobGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())

		id := r.PathValue("id")
		job, err := jobs.GetByID(r.Context(), id)
		if err != nil {
			apierror.WriteStorageError(w, requestID, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ingestionJobView(job))
	}
}
