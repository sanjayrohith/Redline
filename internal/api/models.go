package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/sanjayrohith/redline/internal/apierror"
	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/httpmw"
)

// ModelObject is one entry in the OpenAI-compatible /v1/models listing.
type ModelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ModelListResponse is the OpenAI-compatible /v1/models response body.
type ModelListResponse struct {
	Object string        `json:"object"`
	Data   []ModelObject `json:"data"`
}

// ModelLister is the persistence dependency ModelsHandler needs. It is
// satisfied by *db.ModelRepository; the interface exists so the handler
// is testable without a real database.
type ModelLister interface {
	List(ctx context.Context) ([]db.Model, error)
}

// ModelGetter is the persistence dependency ModelDetailHandler needs. It
// is satisfied by *db.ModelRepository.
type ModelGetter interface {
	GetByID(ctx context.Context, id string) (*db.Model, error)
}

// ModelDetailView is the full ingested-model record a model inspection
// dashboard renders: architecture, parameter count, dtype, VRAM footprint
// per precision, and license attribution.
type ModelDetailView struct {
	ID                    string `json:"id"`
	RepoURL               string `json:"repo_url"`
	Revision              string `json:"revision"`
	RevisionSHA           string `json:"revision_sha"`
	Architecture          string `json:"architecture"`
	ParameterCount        int64  `json:"parameter_count"`
	Dtype                 string `json:"dtype"`
	VRAMEstimateFP16Bytes int64  `json:"vram_estimate_fp16_bytes"`
	VRAMEstimateFP8Bytes  int64  `json:"vram_estimate_fp8_bytes"`
	VRAMEstimateInt4Bytes int64  `json:"vram_estimate_int4_bytes"`
	KVCacheBytes          int64  `json:"kv_cache_bytes"`
	License               string `json:"license"`
	CreatedAt             int64  `json:"created_at"`
}

// ModelCatalogHandler implements GET /v1/dashboard/models: every ingested
// model with its real database id, for the dashboard's own list and
// inspection pages - unlike ModelsHandler's OpenAI-compatible shape,
// whose "id" field is the repo URL and cannot address ModelDetailHandler.
func ModelCatalogHandler(models ModelLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())

		list, err := models.List(r.Context())
		if err != nil {
			apierror.WriteStorageError(w, requestID, err)
			return
		}

		views := make([]ModelDetailView, len(list))
		for i, m := range list {
			views[i] = ModelDetailView{
				ID: m.ID, RepoURL: m.RepoURL, Revision: m.Revision, RevisionSHA: m.RevisionSHA,
				Architecture: m.Architecture, ParameterCount: m.ParameterCount, Dtype: m.Dtype,
				VRAMEstimateFP16Bytes: m.VRAMEstimateFP16Bytes, VRAMEstimateFP8Bytes: m.VRAMEstimateFP8Bytes,
				VRAMEstimateInt4Bytes: m.VRAMEstimateInt4Bytes, KVCacheBytes: m.KVCacheBytes,
				License: m.License, CreatedAt: m.CreatedAt.Unix(),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(struct {
			Data []ModelDetailView `json:"data"`
		}{Data: views})
	}
}

// ModelDetailHandler implements GET /v1/models/{id}: the full record a
// model inspection dashboard renders, including data the OpenAI-compatible
// listing at GET /v1/models deliberately omits (VRAM footprint, license).
func ModelDetailHandler(models ModelGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())

		id := r.PathValue("id")
		m, err := models.GetByID(r.Context(), id)
		if err != nil {
			apierror.WriteStorageError(w, requestID, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ModelDetailView{
			ID: m.ID, RepoURL: m.RepoURL, Revision: m.Revision, RevisionSHA: m.RevisionSHA,
			Architecture: m.Architecture, ParameterCount: m.ParameterCount, Dtype: m.Dtype,
			VRAMEstimateFP16Bytes: m.VRAMEstimateFP16Bytes, VRAMEstimateFP8Bytes: m.VRAMEstimateFP8Bytes,
			VRAMEstimateInt4Bytes: m.VRAMEstimateInt4Bytes, KVCacheBytes: m.KVCacheBytes,
			License: m.License, CreatedAt: m.CreatedAt.Unix(),
		})
	}
}

// ModelsHandler implements GET /v1/models: the catalog of ingested models
// in OpenAI-compatible shape, so existing client SDKs can enumerate them
// without modification.
func ModelsHandler(models ModelLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())

		list, err := models.List(r.Context())
		if err != nil {
			apierror.WriteStorageError(w, requestID, err)
			return
		}

		data := make([]ModelObject, len(list))
		for i, m := range list {
			data[i] = ModelObject{
				ID:      m.RepoURL,
				Object:  "model",
				Created: m.CreatedAt.Unix(),
				OwnedBy: "redline",
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ModelListResponse{Object: "list", Data: data})
	}
}
