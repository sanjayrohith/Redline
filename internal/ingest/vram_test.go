package ingest

import "testing"

func TestComputeVRAMFootprint_WeightsPerPrecision(t *testing.T) {
	const paramCount = 7_000_000_000
	footprint := ComputeVRAMFootprint(paramCount, ModelGeometry{})

	if footprint.FP16.WeightsBytes != paramCount*2 {
		t.Errorf("FP16.WeightsBytes = %d, want %d", footprint.FP16.WeightsBytes, paramCount*2)
	}
	if footprint.FP8.WeightsBytes != paramCount {
		t.Errorf("FP8.WeightsBytes = %d, want %d", footprint.FP8.WeightsBytes, paramCount)
	}
	if footprint.INT4.WeightsBytes != paramCount/2 {
		t.Errorf("INT4.WeightsBytes = %d, want %d", footprint.INT4.WeightsBytes, paramCount/2)
	}
}

func TestComputeVRAMFootprint_INT4RoundsUpOddParamCount(t *testing.T) {
	footprint := ComputeVRAMFootprint(7, ModelGeometry{})
	if footprint.INT4.WeightsBytes != 4 {
		t.Errorf("INT4.WeightsBytes = %d, want 4 (ceil(7/2))", footprint.INT4.WeightsBytes)
	}
}

func TestComputeVRAMFootprint_ZeroGeometryYieldsNoKVCache(t *testing.T) {
	footprint := ComputeVRAMFootprint(1_000_000, ModelGeometry{})
	if footprint.FP16.KVCacheBytes != 0 {
		t.Errorf("KVCacheBytes = %d, want 0 for empty geometry", footprint.FP16.KVCacheBytes)
	}
	if footprint.FP16.TotalBytes != footprint.FP16.WeightsBytes {
		t.Error("TotalBytes should equal WeightsBytes when KVCacheBytes is 0")
	}
}

func TestComputeVRAMFootprint_Llama2_7B_KVCacheMatchesKnownApproximation(t *testing.T) {
	// Llama-2-7b: 32 layers, 32 attention heads (no GQA), hidden_size
	// 4096 (head_dim 128), at a 4096 context. The well-known approximation
	// for this configuration is ~2 GiB of fp16 KV cache.
	geometry := ModelGeometry{
		NumLayers:         32,
		NumAttentionHeads: 32,
		HiddenSize:        4096,
		ContextLength:     4096,
	}

	footprint := ComputeVRAMFootprint(7_000_000_000, geometry)

	const wantKVCacheBytes = 2 * 32 * 32 * 128 * 4096 * 2 // 2 (K+V) * layers * heads * head_dim * ctx * bytes
	if footprint.FP16.KVCacheBytes != wantKVCacheBytes {
		t.Errorf("KVCacheBytes = %d, want %d", footprint.FP16.KVCacheBytes, wantKVCacheBytes)
	}

	const gib = 1 << 30
	if footprint.FP16.KVCacheBytes != 2*gib {
		t.Errorf("KVCacheBytes = %d bytes, want exactly 2 GiB", footprint.FP16.KVCacheBytes)
	}
}

func TestComputeVRAMFootprint_GQAUsesKeyValueHeads(t *testing.T) {
	// Grouped-query attention: fewer KV heads than attention heads should
	// shrink the KV cache proportionally.
	fullGeometry := ModelGeometry{NumLayers: 1, NumAttentionHeads: 8, HiddenSize: 1024, ContextLength: 100}
	gqaGeometry := ModelGeometry{NumLayers: 1, NumAttentionHeads: 8, NumKeyValueHeads: 2, HiddenSize: 1024, ContextLength: 100}

	full := ComputeVRAMFootprint(1000, fullGeometry).FP16.KVCacheBytes
	gqa := ComputeVRAMFootprint(1000, gqaGeometry).FP16.KVCacheBytes

	if gqa != full/4 {
		t.Errorf("gqa KVCacheBytes = %d, want %d (1/4 of full, since 2 kv heads vs 8 attention heads)", gqa, full/4)
	}
}

func TestComputeVRAMFootprint_KVCacheAddedToEveryVariant(t *testing.T) {
	geometry := ModelGeometry{NumLayers: 4, NumAttentionHeads: 4, HiddenSize: 256, ContextLength: 512}
	footprint := ComputeVRAMFootprint(1_000_000, geometry)

	if footprint.FP16.KVCacheBytes == 0 {
		t.Fatal("expected a non-zero KV cache estimate")
	}
	if footprint.FP8.KVCacheBytes != footprint.FP16.KVCacheBytes {
		t.Error("KV cache estimate should be identical across precision variants")
	}
	if footprint.INT4.TotalBytes != footprint.INT4.WeightsBytes+footprint.INT4.KVCacheBytes {
		t.Error("TotalBytes should equal WeightsBytes + KVCacheBytes")
	}
}

func TestModelConfig_Geometry(t *testing.T) {
	cfg := &ModelConfig{
		NumHiddenLayers:       32,
		NumAttentionHeads:     32,
		NumKeyValueHeads:      8,
		HiddenSize:            4096,
		MaxPositionEmbeddings: 8192,
	}
	got := cfg.Geometry()
	want := ModelGeometry{NumLayers: 32, NumAttentionHeads: 32, NumKeyValueHeads: 8, HiddenSize: 4096, ContextLength: 8192}
	if got != want {
		t.Errorf("Geometry() = %+v, want %+v", got, want)
	}
}
