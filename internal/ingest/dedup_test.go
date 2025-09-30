package ingest

import (
	"context"
	"errors"
	"testing"

	"github.com/sanjayrohith/redline/internal/db"
)

type fakeArtifactFinder struct {
	byRevisionSHA map[string]*db.Model
	err           error
}

func (f *fakeArtifactFinder) GetByRevisionSHA(_ context.Context, revisionSHA string) (*db.Model, error) {
	if f.err != nil {
		return nil, f.err
	}
	if m, ok := f.byRevisionSHA[revisionSHA]; ok {
		return m, nil
	}
	return nil, db.ErrNotFound
}

func TestFindExistingArtifact_Found(t *testing.T) {
	existing := &db.Model{ID: "model-1", RevisionSHA: "deadbeef"}
	finder := &fakeArtifactFinder{byRevisionSHA: map[string]*db.Model{"deadbeef": existing}}

	got, err := FindExistingArtifact(context.Background(), finder, "deadbeef")
	if err != nil {
		t.Fatalf("FindExistingArtifact() error = %v", err)
	}
	if got != existing {
		t.Errorf("got %+v, want %+v", got, existing)
	}
}

func TestFindExistingArtifact_NotFoundIsNotAnError(t *testing.T) {
	finder := &fakeArtifactFinder{byRevisionSHA: map[string]*db.Model{}}

	got, err := FindExistingArtifact(context.Background(), finder, "unknown-sha")
	if err != nil {
		t.Fatalf("FindExistingArtifact() error = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("got = %+v, want nil", got)
	}
}

func TestFindExistingArtifact_PropagatesOtherErrors(t *testing.T) {
	finder := &fakeArtifactFinder{err: errors.New("connection reset")}

	if _, err := FindExistingArtifact(context.Background(), finder, "deadbeef"); err == nil {
		t.Fatal("FindExistingArtifact() error = nil, want propagated error")
	}
}
