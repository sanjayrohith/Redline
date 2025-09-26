package apierror

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sanjayrohith/redline/internal/db"
)

func TestWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, http.StatusTeapot, CodeValidation, "bad input", "req-1")

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}

	var env Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Error.Code != string(CodeValidation) || env.Error.Message != "bad input" || env.Error.RequestID != "req-1" {
		t.Errorf("envelope = %+v", env)
	}
}

func TestWriteStorageError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   Code
	}{
		{"not found", db.ErrNotFound, http.StatusNotFound, CodeNotFound},
		{"conflict", db.ErrConflict, http.StatusConflict, CodeConflict},
		{"wrapped not found", errors.New("lookup failed: " + db.ErrNotFound.Error()), http.StatusInternalServerError, CodeInternal},
		{"unknown", errors.New("boom"), http.StatusInternalServerError, CodeInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			WriteStorageError(rec, "req-1", tt.err)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			var env Envelope
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if env.Error.Code != string(tt.wantCode) {
				t.Errorf("code = %q, want %q", env.Error.Code, tt.wantCode)
			}
		})
	}
}

func TestWriteStorageError_UnwrapsWrappedSentinels(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), db.ErrNotFound)

	rec := httptest.NewRecorder()
	WriteStorageError(rec, "req-1", wrapped)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
