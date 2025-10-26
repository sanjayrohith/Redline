package inference_test

import (
	"testing"

	"github.com/sanjayrohith/redline/internal/inference"
)

func TestRenderBatchingConfig_MaxNumSeqsTracksCacheCapacity(t *testing.T) {
	kv := inference.KVCacheConfig{BlockSize: 16, NumGPUBlocks: 1000} // 16000 total cache tokens
	cfg := inference.RenderBatchingConfig(kv, 4000)

	// 16000 / 4000 = 4 full-length sequences fit concurrently.
	if cfg.MaxNumSeqs != 4 {
		t.Errorf("MaxNumSeqs = %d, want 4", cfg.MaxNumSeqs)
	}
}

func TestRenderBatchingConfig_MaxNumSeqsNeverGoesBelowOne(t *testing.T) {
	kv := inference.KVCacheConfig{BlockSize: 16, NumGPUBlocks: 10} // 160 total cache tokens
	cfg := inference.RenderBatchingConfig(kv, 4000)                // one sequence alone exceeds cache capacity

	if cfg.MaxNumSeqs != 1 {
		t.Errorf("MaxNumSeqs = %d, want 1 (never zero, even when a single sequence exceeds cache capacity)", cfg.MaxNumSeqs)
	}
}

func TestRenderBatchingConfig_MaxNumBatchedTokensCappedByCeiling(t *testing.T) {
	kv := inference.KVCacheConfig{BlockSize: 16, NumGPUBlocks: 100000} // huge cache
	cfg := inference.RenderBatchingConfig(kv, 4000)

	if cfg.MaxNumBatchedTokens != inference.DefaultMaxBatchedTokenCeiling {
		t.Errorf("MaxNumBatchedTokens = %d, want the ceiling %d", cfg.MaxNumBatchedTokens, inference.DefaultMaxBatchedTokenCeiling)
	}
}

func TestRenderBatchingConfig_MaxNumBatchedTokensTracksSmallCache(t *testing.T) {
	kv := inference.KVCacheConfig{BlockSize: 16, NumGPUBlocks: 10} // 160 total cache tokens, well under the ceiling
	cfg := inference.RenderBatchingConfig(kv, 4000)

	if cfg.MaxNumBatchedTokens != 160 {
		t.Errorf("MaxNumBatchedTokens = %d, want 160 (bounded by cache capacity, not the ceiling)", cfg.MaxNumBatchedTokens)
	}
}
