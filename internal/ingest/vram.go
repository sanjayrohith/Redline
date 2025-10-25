package ingest

// VRAMEstimate is one precision variant's memory footprint: the weights
// themselves plus the KV cache they need at the configured context length.
type VRAMEstimate struct {
	WeightsBytes int64
	KVCacheBytes int64
	TotalBytes   int64
}

// VRAMFootprint is a model's memory footprint across every precision this
// gateway can serve it at.
type VRAMFootprint struct {
	FP16 VRAMEstimate
	FP8  VRAMEstimate
	INT4 VRAMEstimate
}

// ModelGeometry is the layer and attention geometry needed to size a KV
// cache, drawn from config.json.
type ModelGeometry struct {
	NumLayers         int
	NumAttentionHeads int
	NumKeyValueHeads  int // 0 means "same as NumAttentionHeads" (no GQA).
	HiddenSize        int
	ContextLength     int
}

const (
	bytesPerParamFP16 = 2
	bytesPerParamFP8  = 1

	// kvCacheBytesPerElement: the KV cache is kept at fp16 regardless of
	// weight quantization in every mainstream serving engine, since
	// quantizing it independently trades a large accuracy hit for a
	// comparatively small memory saving.
	kvCacheBytesPerElement = 2
	kAndVTensors           = 2
)

// ComputeVRAMFootprint computes the exact weight memory for FP16, FP8, and
// INT4, adds a KV-cache estimate derived from geometry to each, and
// returns all three variants together.
func ComputeVRAMFootprint(paramCount int64, geometry ModelGeometry) VRAMFootprint {
	kv := kvCacheBytes(geometry)

	build := func(weightsBytes int64) VRAMEstimate {
		return VRAMEstimate{WeightsBytes: weightsBytes, KVCacheBytes: kv, TotalBytes: weightsBytes + kv}
	}

	return VRAMFootprint{
		FP16: build(paramCount * bytesPerParamFP16),
		FP8:  build(paramCount * bytesPerParamFP8),
		INT4: build(int4WeightBytes(paramCount)),
	}
}

// int4WeightBytes rounds up: INT4 packs two parameters per byte, so an odd
// parameter count still occupies a whole trailing byte.
func int4WeightBytes(paramCount int64) int64 {
	return (paramCount + 1) / 2
}

func kvCacheBytes(g ModelGeometry) int64 {
	if g.ContextLength <= 0 {
		return 0
	}
	return KVCacheBytesPerToken(g) * int64(g.ContextLength)
}

// KVCacheBytesPerToken is the KV cache memory one token of context costs
// across every layer, at the fp16 precision the cache is always kept at
// (see kvCacheBytesPerElement). Exported so callers sizing a KV cache in
// fixed-size blocks - PagedAttention's block allocator, for one - can
// derive bytes-per-block as KVCacheBytesPerToken(g) * tokensPerBlock
// without duplicating this geometry math.
func KVCacheBytesPerToken(g ModelGeometry) int64 {
	if g.NumLayers <= 0 || g.NumAttentionHeads <= 0 || g.HiddenSize <= 0 {
		return 0
	}

	kvHeads := g.NumKeyValueHeads
	if kvHeads <= 0 {
		kvHeads = g.NumAttentionHeads
	}
	headDim := g.HiddenSize / g.NumAttentionHeads

	return int64(kAndVTensors) * int64(g.NumLayers) * int64(kvHeads) * int64(headDim) * int64(kvCacheBytesPerElement)
}
