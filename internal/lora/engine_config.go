package lora

// EngineConfig is vLLM's multi-LoRA serving tuning:
// https://docs.vllm.ai/en/latest/features/lora.html. Serving adapters
// this way keeps one base model resident and layers each adapter's
// low-rank matrices on top of it per-request, so the marginal cost of
// adding a variant is the adapter's few megabytes, not another full copy
// of the base model's weights.
type EngineConfig struct {
	// Enabled is --enable-lora.
	Enabled bool
	// MaxLoRAs is --max-loras: how many adapters may be resident and
	// servable concurrently.
	MaxLoRAs int
	// MaxLoRARank is --max-lora-rank: the largest rank any resident
	// adapter may have. vLLM sizes its LoRA memory pool for this ceiling
	// up front, so it must cover every adapter this deployment intends
	// to hold, not just the first one loaded.
	MaxLoRARank int
}

// RenderEngineConfig derives vLLM's multi-LoRA engine config from how
// many adapters this deployment needs to hold resident at once
// (maxResidentAdapters) and the ranks of the adapters it is being
// configured to serve. An empty deployment - no slots requested, or no
// adapters to size the rank ceiling from - leaves LoRA serving disabled
// entirely, matching a base-model-only deployment's engine args exactly.
func RenderEngineConfig(maxResidentAdapters int, ranks []int) EngineConfig {
	if maxResidentAdapters <= 0 || len(ranks) == 0 {
		return EngineConfig{}
	}

	maxRank := 0
	for _, r := range ranks {
		if r > maxRank {
			maxRank = r
		}
	}

	return EngineConfig{
		Enabled:     true,
		MaxLoRAs:    maxResidentAdapters,
		MaxLoRARank: maxRank,
	}
}
