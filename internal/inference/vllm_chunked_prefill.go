package inference

// ChunkedPrefillConfig is vLLM's chunked prefill tuning:
// https://docs.vllm.ai/en/latest/serving/engine_args.html
// (--enable-chunked-prefill).
type ChunkedPrefillConfig struct {
	// Enabled is --enable-chunked-prefill.
	Enabled bool
}

// RenderChunkedPrefillConfig decides whether chunked prefill needs to be
// on for a deployment: it matters only when a full-length prompt
// (maxModelLen tokens) could exceed one scheduling step's token budget
// (batching.MaxNumBatchedTokens, from RenderBatchingConfig). Only then
// would prefilling one long prompt in a single step monopolize that
// step and spike every other concurrent request's time to first token -
// which chunked prefill fixes by splitting the prompt across multiple
// steps, interleaved with other sequences' decode steps, instead. When
// every prompt this deployment can receive already fits inside one
// step's budget, there is nothing for chunking to mitigate.
func RenderChunkedPrefillConfig(maxModelLen int, batching BatchingConfig) ChunkedPrefillConfig {
	return ChunkedPrefillConfig{Enabled: maxModelLen > batching.MaxNumBatchedTokens}
}
