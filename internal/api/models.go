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
