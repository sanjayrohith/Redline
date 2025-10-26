package inference

import "strings"

// IsCUDAOutOfMemory reports whether err represents vLLM signaling a CUDA
// out-of-memory condition - whether raised while compiling CUDA graphs
// during warmup or while generating - as opposed to a network error,
// timeout, or an unrelated server error that also happens to return a
// non-200 status. vLLM reports both as a plain HTTP 500 with no distinct
// status code or header of its own, so the only place this distinction
// exists is the error text PyTorch's CUDA allocator raises, which vLLM
// passes through into the response body newVLLMStatusError captures.
func IsCUDAOutOfMemory(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "cuda") && strings.Contains(msg, "out of memory")
}
