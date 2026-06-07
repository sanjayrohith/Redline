package ingest

import (
	"context"
	"errors"
	"testing"

	"github.com/sanjayrohith/redline/internal/db"
)

type fakeManifestFetcher struct {
	manifest   *Manifest
	config     *ModelConfig
	manifestFn func(ctx context.Context, ref Reference) (*Manifest, error)
}

func (f *fakeManifestFetcher) FetchManifest(ctx context.Context, ref Reference) (*Manifest, error) {
	if f.manifestFn != nil {
		return f.manifestFn(ctx, ref)
	}
	return f.manifest, nil
}

func (f *fakeManifestFetcher) FetchConfig(context.Context, Reference) (*ModelConfig, error) {
	return f.config, nil
}

func (f *fakeManifestFetcher) FileURL(ref Reference, path string) string {
	return "https://example.com/" + ref.RepoID() + "/" + path
}

type fakeJobStore struct {
	states    []string
	completed *string
	failed    *string
}

func (f *fakeJobStore) UpdateState(_ context.Context, _, state string) error {
	f.states = append(f.states, state)
	return nil
}

func (f *fakeJobStore) UpdateProgress(_ context.Context, _ string, _, _ int64) error {
	return nil
}

func (f *fakeJobStore) Complete(_ context.Context, _, modelID string) error {
	f.completed = &modelID
	return nil
}

func (f *fakeJobStore) Fail(_ context.Context, _, errMsg string) error {
	f.failed = &errMsg
	return nil
}

type fakeModelStore struct {
	created *db.NewModel
}

func (f *fakeModelStore) Create(_ context.Context, m db.NewModel) (*db.Model, error) {
	f.created = &m
	return &db.Model{ID: "model-1", RepoURL: m.RepoURL, RevisionSHA: m.RevisionSHA}, nil
}

func TestOrchestrator_Run_EndToEndSuccess(t *testing.T) {
	headerJSON := `{
		"weight": {"dtype": "F16", "shape": [4096, 4096], "data_offsets": [0, 33554432]}
	}`
	fetcher := &fakeRangeFetcher{data: fakeSafetensorsFile(headerJSON, 1024)}
	manifests := &fakeManifestFetcher{
		manifest: &Manifest{
			RevisionSHA: "sha-abc",
			Files: []ManifestFile{
				{Path: "config.json", Size: 100},
				{Path: "model.safetensors", Size: 16_000_000},
			},
		},
		config: &ModelConfig{ModelType: "llama", NumHiddenLayers: 32, NumAttentionHeads: 32, HiddenSize: 4096},
	}
	jobs := &fakeJobStore{}
	models := &fakeModelStore{}
	dedup := &fakeArtifactFinder{}

	o := NewOrchestrator(manifests, fetcher, jobs, models, dedup, DefaultQuota)
	ref := Reference{Owner: "org", Name: "model", Revision: "main"}

	if err := o.Run(context.Background(), "job-1", ref); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if models.created == nil {
		t.Fatal("model was not created")
	}
	if models.created.Architecture != "llama" {
		t.Errorf("Architecture = %q, want llama", models.created.Architecture)
	}
	if models.created.RevisionSHA != "sha-abc" {
		t.Errorf("RevisionSHA = %q, want sha-abc", models.created.RevisionSHA)
	}
	if models.created.ParameterCount != 4096*4096 {
		t.Errorf("ParameterCount = %d, want %d", models.created.ParameterCount, 4096*4096)
	}
	if jobs.completed == nil || *jobs.completed != "model-1" {
		t.Errorf("completed = %v, want model-1", jobs.completed)
	}
	if jobs.failed != nil {
		t.Errorf("failed = %v, want nil", *jobs.failed)
	}

	var sawVerifying bool
	for _, s := range jobs.states {
		if s == db.IngestionStateVerifying {
			sawVerifying = true
		}
	}
	if !sawVerifying {
		t.Errorf("states = %v, want it to include %q", jobs.states, db.IngestionStateVerifying)
	}
}

func TestOrchestrator_Run_DeduplicatesExistingRevision(t *testing.T) {
	manifests := &fakeManifestFetcher{manifest: &Manifest{RevisionSHA: "sha-existing", Files: []ManifestFile{
		{Path: "model.safetensors", Size: 1000},
	}}}
	jobs := &fakeJobStore{}
	models := &fakeModelStore{}
	dedup := &fakeArtifactFinder{byRevisionSHA: map[string]*db.Model{"sha-existing": {ID: "model-existing", RevisionSHA: "sha-existing"}}}

	o := NewOrchestrator(manifests, &fakeRangeFetcher{}, jobs, models, dedup, DefaultQuota)

	if err := o.Run(context.Background(), "job-2", Reference{Owner: "org", Name: "model", Revision: "main"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if models.created != nil {
		t.Error("a new model was created for an already-ingested revision")
	}
	if jobs.completed == nil || *jobs.completed != "model-existing" {
		t.Errorf("completed = %v, want model-existing", jobs.completed)
	}
}

func TestOrchestrator_Run_FailsJobWhenNoWeightFile(t *testing.T) {
	manifests := &fakeManifestFetcher{manifest: &Manifest{RevisionSHA: "sha-x", Files: []ManifestFile{
		{Path: "README.md", Size: 10},
	}}}
	jobs := &fakeJobStore{}
	o := NewOrchestrator(manifests, &fakeRangeFetcher{}, jobs, &fakeModelStore{}, &fakeArtifactFinder{}, DefaultQuota)

	err := o.Run(context.Background(), "job-3", Reference{Owner: "org", Name: "model", Revision: "main"})
	if err == nil {
		t.Fatal("Run() error = nil, want an error for a repository with no safetensors file")
	}
	if jobs.failed == nil {
		t.Error("job was not marked failed")
	}
}

func TestOrchestrator_Run_FailsJobWhenManifestFetchErrors(t *testing.T) {
	manifests := &fakeManifestFetcher{manifestFn: func(context.Context, Reference) (*Manifest, error) {
		return nil, errors.New("upstream unreachable")
	}}
	jobs := &fakeJobStore{}
	o := NewOrchestrator(manifests, &fakeRangeFetcher{}, jobs, &fakeModelStore{}, &fakeArtifactFinder{}, DefaultQuota)

	err := o.Run(context.Background(), "job-4", Reference{Owner: "org", Name: "model", Revision: "main"})
	if err == nil {
		t.Fatal("Run() error = nil, want an error")
	}
	if jobs.failed == nil {
		t.Error("job was not marked failed")
	}
}
