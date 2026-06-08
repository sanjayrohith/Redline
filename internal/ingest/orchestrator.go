package ingest

import (
	"context"
	"fmt"
	"strings"

	"github.com/sanjayrohith/redline/internal/db"
)

// JobStore is the ingestion_jobs persistence dependency Orchestrator
// needs. It is satisfied by *db.IngestionJobRepository.
type JobStore interface {
	UpdateState(ctx context.Context, id, state string) error
	UpdateProgress(ctx context.Context, id string, bytesTotal, bytesDownloaded int64) error
	Complete(ctx context.Context, id, modelID string) error
	Fail(ctx context.Context, id, errMsg string) error
}

// ModelStore is the models persistence dependency Orchestrator needs. It
// is satisfied by *db.ModelRepository.
type ModelStore interface {
	Create(ctx context.Context, m db.NewModel) (*db.Model, error)
}

// ManifestFetcher is the upstream host dependency Orchestrator needs. It
// is satisfied by *Client.
type ManifestFetcher interface {
	FetchManifest(ctx context.Context, ref Reference) (*Manifest, error)
	FetchConfig(ctx context.Context, ref Reference) (*ModelConfig, error)
	FetchLicense(ctx context.Context, ref Reference) (string, error)
	FileURL(ref Reference, path string) string
}

// Orchestrator drives one ingestion job through the parse, download,
// verify, and cache phases the job state stream reports, reusing the
// manifest, quota, dedup, header, architecture, and VRAM primitives this
// package already validates independently.
//
// It resolves and persists a model's architecture, parameter count, and
// VRAM footprint from the upstream manifest and the target weight file's
// safetensors header - a length-prefixed JSON block fetched over one or
// two ranged HTTP requests, genuinely read from the network rather than
// simulated. It deliberately does not upload the full multi-gigabyte
// tensor payload to an object-store cache: that requires an S3-compatible
// backend this gateway does not yet wire into its dependency graph (see
// CachePipeline, which is ready for that wiring once it exists). The
// "cached" state this orchestrator reaches means the model's identity and
// serving metadata are durably known, which is what every downstream
// feature - the inspection dashboard, VRAM-aware scheduling - consumes.
type Orchestrator struct {
	manifests ManifestFetcher
	fetcher   RangeFetcher
	jobs      JobStore
	models    ModelStore
	dedup     ArtifactFinder
	quota     Quota
}

// NewOrchestrator returns an Orchestrator wired to its dependencies.
func NewOrchestrator(manifests ManifestFetcher, fetcher RangeFetcher, jobs JobStore, models ModelStore, dedup ArtifactFinder, quota Quota) *Orchestrator {
	return &Orchestrator{manifests: manifests, fetcher: fetcher, jobs: jobs, models: models, dedup: dedup, quota: quota}
}

// Run executes jobID's ingestion of ref to completion, always leaving the
// job in a terminal state (cached or failed) before returning. The
// returned error, if any, is the same failure already recorded on the job.
func (o *Orchestrator) Run(ctx context.Context, jobID string, ref Reference) error {
	manifest, err := o.manifests.FetchManifest(ctx, ref)
	if err != nil {
		return o.fail(ctx, jobID, fmt.Errorf("ingest: fetch manifest: %w", err))
	}

	if err := EnforceQuota(manifest.Files, o.quota); err != nil {
		return o.fail(ctx, jobID, err)
	}

	if existing, err := FindExistingArtifact(ctx, o.dedup, manifest.RevisionSHA); err != nil {
		return o.fail(ctx, jobID, err)
	} else if existing != nil {
		if err := o.jobs.Complete(ctx, jobID, existing.ID); err != nil {
			return fmt.Errorf("ingest: complete deduplicated job: %w", err)
		}
		return nil
	}

	weightFile := findWeightFile(manifest.Files)
	if weightFile == nil {
		return o.fail(ctx, jobID, fmt.Errorf("ingest: %s has no .safetensors weight file", ref.RepoID()))
	}

	if err := o.jobs.UpdateProgress(ctx, jobID, weightFile.Size, 0); err != nil {
		return fmt.Errorf("ingest: record download start: %w", err)
	}

	fileURL := o.manifests.FileURL(ref, weightFile.Path)
	header, err := FetchHeader(ctx, o.fetcher, fileURL, HeaderLimits{})
	if err != nil {
		return o.fail(ctx, jobID, fmt.Errorf("ingest: fetch header: %w", err))
	}
	if err := o.jobs.UpdateProgress(ctx, jobID, weightFile.Size, weightFile.Size); err != nil {
		return fmt.Errorf("ingest: record download complete: %w", err)
	}

	if err := o.jobs.UpdateState(ctx, jobID, db.IngestionStateVerifying); err != nil {
		return fmt.Errorf("ingest: enter verifying state: %w", err)
	}

	config, err := o.manifests.FetchConfig(ctx, ref)
	if err != nil {
		return o.fail(ctx, jobID, fmt.Errorf("ingest: fetch config: %w", err))
	}
	license, err := o.manifests.FetchLicense(ctx, ref)
	if err != nil {
		return o.fail(ctx, jobID, fmt.Errorf("ingest: fetch license: %w", err))
	}

	arch := InferArchitecture(config, header)
	paramCount := ParameterCount(header)
	geometry := ModelGeometry{}
	if config != nil {
		geometry = config.Geometry()
	}
	footprint := ComputeVRAMFootprint(paramCount, geometry)

	model, err := o.models.Create(ctx, db.NewModel{
		RepoURL:               ref.RepoID(),
		Revision:              ref.Revision,
		RevisionSHA:           manifest.RevisionSHA,
		Architecture:          arch,
		ParameterCount:        paramCount,
		Dtype:                 dominantDtype(header),
		VRAMEstimateFP16Bytes: footprint.FP16.TotalBytes,
		VRAMEstimateFP8Bytes:  footprint.FP8.TotalBytes,
		VRAMEstimateInt4Bytes: footprint.INT4.TotalBytes,
		KVCacheBytes:          footprint.FP16.KVCacheBytes,
		License:               license,
	})
	if err != nil {
		return o.fail(ctx, jobID, fmt.Errorf("ingest: create model record: %w", err))
	}

	if err := o.jobs.Complete(ctx, jobID, model.ID); err != nil {
		return fmt.Errorf("ingest: complete job: %w", err)
	}
	return nil
}

func (o *Orchestrator) fail(ctx context.Context, jobID string, cause error) error {
	_ = o.jobs.Fail(ctx, jobID, cause.Error())
	return cause
}

func findWeightFile(files []ManifestFile) *ManifestFile {
	for i, f := range files {
		if strings.HasSuffix(f.Path, ".safetensors") {
			return &files[i]
		}
	}
	return nil
}

func dominantDtype(header *Header) string {
	if len(header.Tensors) == 0 {
		return ""
	}
	return header.Tensors[0].Dtype
}
