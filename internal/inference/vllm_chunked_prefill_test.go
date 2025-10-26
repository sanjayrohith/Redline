package inference_test

import (
	"testing"

	"github.com/sanjayrohith/redline/internal/inference"
)

func TestRenderChunkedPrefillConfig_EnabledWhenPromptCanExceedStepBudget(t *testing.T) {
	batching := inference.BatchingConfig{MaxNumBatchedTokens: 4096}
	cfg := inference.RenderChunkedPrefillConfig(32000, batching)

	if !cfg.Enabled {
		t.Error("Enabled = false, want true when max model len exceeds the step token budget")
	}
}

func TestRenderChunkedPrefillConfig_DisabledWhenEveryPromptFitsInOneStep(t *testing.T) {
	batching := inference.BatchingConfig{MaxNumBatchedTokens: 8192}
	cfg := inference.RenderChunkedPrefillConfig(4096, batching)

	if cfg.Enabled {
		t.Error("Enabled = true, want false when max model len already fits inside the step token budget")
	}
}

func TestRenderChunkedPrefillConfig_DisabledAtExactBoundary(t *testing.T) {
	batching := inference.BatchingConfig{MaxNumBatchedTokens: 8192}
	cfg := inference.RenderChunkedPrefillConfig(8192, batching)

	if cfg.Enabled {
		t.Error("Enabled = true, want false when max model len exactly equals the step token budget")
	}
}
