package ingest

import (
	"context"
	"errors"
	"fmt"

	"github.com/sanjayrohith/redline/internal/db"
)

// ArtifactFinder looks up an already-cached model by its immutable
// revision digest. It is satisfied by *db.ModelRepository; the interface
// exists so dedup logic is testable without a real database.
type ArtifactFinder interface {
	GetByRevisionSHA(ctx context.Context, revisionSHA string) (*db.Model, error)
}

// FindExistingArtifact reports whether a model has already been ingested
// at revisionSHA, returning it if so and (nil, nil) if not - a repository
// lookup miss is the expected, non-error outcome of this check.
func FindExistingArtifact(ctx context.Context, finder ArtifactFinder, revisionSHA string) (*db.Model, error) {
	model, err := finder.GetByRevisionSHA(ctx, revisionSHA)
	if errors.Is(err, db.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ingest: look up existing artifact: %w", err)
	}
	return model, nil
}
