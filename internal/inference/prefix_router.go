package inference

// PrefixRouter routes a completion request to the SGLang backend when its
// leading system message matches one of a registered set of shared
// system prompts - exactly the workload SGLang's RadixAttention prefix
// cache is built to exploit, since many requests reusing the same system
// prompt get that prefix's KV cache computed once instead of once per
// request. Every other request routes to the default backend.
type PrefixRouter struct {
	sglang         Backend
	fallback       Backend
	sharedPrefixes map[string]bool
}

// NewPrefixRouter returns a PrefixRouter that sends requests whose first
// message is a system message matching one of sharedSystemPrompts to
// sglang, and everything else to fallback.
func NewPrefixRouter(sglang, fallback Backend, sharedSystemPrompts []string) *PrefixRouter {
	prefixes := make(map[string]bool, len(sharedSystemPrompts))
	for _, p := range sharedSystemPrompts {
		prefixes[p] = true
	}
	return &PrefixRouter{sglang: sglang, fallback: fallback, sharedPrefixes: prefixes}
}

// Select returns the backend req should be dispatched to.
func (r *PrefixRouter) Select(req CompletionRequest) Backend {
	if len(req.Messages) > 0 && req.Messages[0].Role == "system" && r.sharedPrefixes[req.Messages[0].Content] {
		return r.sglang
	}
	return r.fallback
}
