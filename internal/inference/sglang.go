package inference

import "net/http"

// SGLangBackend implements Backend against a live SGLang server. SGLang's
// /v1/chat/completions endpoint speaks the same OpenAI-compatible chat
// completions and SSE streaming wire protocol vLLM's does, so it reuses
// VLLMBackend's HTTP implementation outright rather than duplicating it;
// it exists as its own Go type so callers routing between backends (see
// PrefixRouter) have a distinct type to route to, and so each backend's
// own configuration - SGLang's default port is 30000, vLLM's is 8000 -
// stays separate at the call site.
type SGLangBackend struct {
	*VLLMBackend
}

var _ Backend = (*SGLangBackend)(nil)

// NewSGLangBackend returns a SGLangBackend against baseURL.
//
// SGLang's differentiator over vLLM is RadixAttention: an automatic
// prefix-caching KV cache organized as a radix tree, shared *across*
// requests rather than scoped to one request's own generation the way
// vLLM's PagedAttention cache is. A shared system prompt sent by many
// different requests has its KV cache computed once and reused by every
// subsequent request with the same prefix, rather than recomputed per
// request - which is the workload this backend exists to be routed to
// (see PrefixRouter).
func NewSGLangBackend(baseURL string, httpClient *http.Client) *SGLangBackend {
	return &SGLangBackend{VLLMBackend: NewVLLMBackend(baseURL, httpClient)}
}
