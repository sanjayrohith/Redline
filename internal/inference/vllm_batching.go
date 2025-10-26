package inference

// DefaultMaxBatchedTokenCeiling caps --max-num-batched-tokens
// independent of KV cache size: beyond roughly this many tokens in a
// single forward pass, one oversized prefill dominates a scheduling step
// and starves every other in-flight sequence's decode step of a turn,
// which is continuous batching in name only. It is a per-step budget,
// not a memory bound - the KV cache is what actually bounds how many
// tokens can be resident across all sequences at once.
const DefaultMaxBatchedTokenCeiling = 8192

// BatchingConfig is vLLM's continuous-batching tuning:
// https://docs.vllm.ai/en/latest/serving/engine_args.html. Continuous
// batching itself is not a flag to toggle - it is how vLLM's scheduler
// always operates, admitting new sequences into a running batch at every
// decode step rather than waiting for the batch to drain - so "enabling"
// it means tuning the two knobs that make it behave well rather than
// serialize under load, which is what this config computes.
type BatchingConfig struct {
	// MaxNumBatchedTokens is --max-num-batched-tokens: the token budget
	// for one scheduling step, shared across every sequence the
	// scheduler advances that step (prefill and decode alike).
	MaxNumBatchedTokens int
	// MaxNumSeqs is --max-num-seqs: how many sequences may run
	// concurrently, bounded by how many full-length sequences the KV
	// cache can actually hold at once - continuous batching cannot admit
	// more concurrency than there is cache capacity for, regardless of
	// what the token budget alone would allow.
	MaxNumSeqs int
}

// RenderBatchingConfig derives continuous batching's tuning from the KV
// cache this deployment was actually sized with (RenderKVCacheConfig)
// and the context length it serves (maxModelLen, from RenderEngineArgs):
// grounded in the real, computed cache capacity rather than a fixed
// guess, so the batch budget tracks whatever tensor-parallel degree and
// precision this deployment landed on.
func RenderBatchingConfig(kv KVCacheConfig, maxModelLen int) BatchingConfig {
	totalCacheTokens := kv.BlockSize * kv.NumGPUBlocks

	maxNumSeqs := 1
	if maxModelLen > 0 {
		maxNumSeqs = totalCacheTokens / maxModelLen
		if maxNumSeqs < 1 {
			maxNumSeqs = 1
		}
	}

	maxNumBatchedTokens := totalCacheTokens
	if maxNumBatchedTokens > DefaultMaxBatchedTokenCeiling {
		maxNumBatchedTokens = DefaultMaxBatchedTokenCeiling
	}
	if maxNumBatchedTokens < 1 {
		maxNumBatchedTokens = 1
	}

	return BatchingConfig{
		MaxNumBatchedTokens: maxNumBatchedTokens,
		MaxNumSeqs:          maxNumSeqs,
	}
}
