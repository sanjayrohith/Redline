package lora_test

import (
	"testing"

	"github.com/sanjayrohith/redline/internal/lora"
)

func TestRenderEngineConfig_DisabledWithNoAdapters(t *testing.T) {
	cfg := lora.RenderEngineConfig(4, nil)
	if cfg.Enabled {
		t.Error("Enabled = true, want false with no adapters")
	}
}

func TestRenderEngineConfig_DisabledWithZeroSlots(t *testing.T) {
	cfg := lora.RenderEngineConfig(0, []int{16})
	if cfg.Enabled {
		t.Error("Enabled = true, want false with zero resident slots requested")
	}
}

func TestRenderEngineConfig_SizesMaxLoRARankToTheLargestAdapter(t *testing.T) {
	cfg := lora.RenderEngineConfig(4, []int{8, 32, 16})

	if !cfg.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if cfg.MaxLoRAs != 4 {
		t.Errorf("MaxLoRAs = %d, want 4", cfg.MaxLoRAs)
	}
	if cfg.MaxLoRARank != 32 {
		t.Errorf("MaxLoRARank = %d, want 32 (the largest declared rank)", cfg.MaxLoRARank)
	}
}
