package inference_test

import (
	"errors"
	"testing"

	"github.com/sanjayrohith/redline/internal/ingest"

	"github.com/sanjayrohith/redline/internal/inference"
)

func TestRenderKVCacheConfig_SizesWholeBlocksFromBudget(t *testing.T) {
	// Llama-2-7B-shaped geometry: 32 layers, 32 heads, no GQA, hidden 4096.
	geometry := ingest.ModelGeometry{NumLayers: 32, NumAttentionHeads: 32, HiddenSize: 4096}
	bytesPerToken := ingest.KVCacheBytesPerToken(geometry)
	bytesPerBlock := bytesPerToken * inference.KVCacheBlockSize

	// Budget for exactly 10 blocks plus some slack that must not round up
	// to an 11th, since that block would exceed the real memory budget.
	budget := bytesPerBlock*10 + bytesPerBlock/2

	cfg, err := inference.RenderKVCacheConfig(geometry, budget)
	if err != nil {
		t.Fatalf("RenderKVCacheConfig() error = %v", err)
	}
	if cfg.BlockSize != inference.KVCacheBlockSize {
		t.Errorf("BlockSize = %d, want %d", cfg.BlockSize, inference.KVCacheBlockSize)
	}
	if cfg.NumGPUBlocks != 10 {
		t.Errorf("NumGPUBlocks = %d, want 10 (truncated, not rounded up)", cfg.NumGPUBlocks)
	}
}

func TestRenderKVCacheConfig_RefusesABudgetUnderOneBlock(t *testing.T) {
	geometry := ingest.ModelGeometry{NumLayers: 32, NumAttentionHeads: 32, HiddenSize: 4096}
	bytesPerToken := ingest.KVCacheBytesPerToken(geometry)
	tooSmall := bytesPerToken*inference.KVCacheBlockSize - 1

	_, err := inference.RenderKVCacheConfig(geometry, tooSmall)
	if !errors.Is(err, inference.ErrKVCacheBudgetTooSmall) {
		t.Fatalf("RenderKVCacheConfig() error = %v, want ErrKVCacheBudgetTooSmall", err)
	}
}

func TestRenderKVCacheConfig_RejectsInvalidGeometry(t *testing.T) {
	if _, err := inference.RenderKVCacheConfig(ingest.ModelGeometry{}, 1<<30); err == nil {
		t.Fatal("RenderKVCacheConfig() with zero-value geometry error = nil, want an error")
	}
}

func TestRenderKVCacheConfig_GQAUsesFewerBytesPerBlockThanMHA(t *testing.T) {
	mha := ingest.ModelGeometry{NumLayers: 32, NumAttentionHeads: 32, HiddenSize: 4096}
	gqa := ingest.ModelGeometry{NumLayers: 32, NumAttentionHeads: 32, NumKeyValueHeads: 8, HiddenSize: 4096}

	budget := int64(1) << 34 // 16GiB, large enough for many blocks either way

	mhaCfg, err := inference.RenderKVCacheConfig(mha, budget)
	if err != nil {
		t.Fatalf("RenderKVCacheConfig(mha) error = %v", err)
	}
	gqaCfg, err := inference.RenderKVCacheConfig(gqa, budget)
	if err != nil {
		t.Fatalf("RenderKVCacheConfig(gqa) error = %v", err)
	}

	if gqaCfg.NumGPUBlocks <= mhaCfg.NumGPUBlocks {
		t.Errorf("GQA blocks = %d, want more than MHA blocks = %d for the same budget", gqaCfg.NumGPUBlocks, mhaCfg.NumGPUBlocks)
	}
}
