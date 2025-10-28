package lora

import (
	"errors"
	"fmt"
	"sync"
)

// ErrAdapterNotLoaded means the requested model identifier names an
// adapter that is not currently resident on the engine serving this
// deployment.
var ErrAdapterNotLoaded = errors.New("lora: adapter not loaded")

// Router maps a chat completion request's model identifier onto the
// resident target it should actually be served by: the base model
// itself, or one of the adapters currently loaded on top of it.
type Router struct {
	baseModel string

	mu       sync.RWMutex
	resident map[string]bool
}

// NewRouter returns a Router for a deployment whose resident base model
// is baseModel.
func NewRouter(baseModel string) *Router {
	return &Router{baseModel: baseModel, resident: make(map[string]bool)}
}

// Resolve returns the target a request naming requestedModel should be
// routed to. requestedModel matching the base model itself always
// resolves; any other identifier must name a currently resident
// adapter, or Resolve returns ErrAdapterNotLoaded - a load/unload race
// (the adapter existing but not yet loaded, or just evicted) is a
// legitimate, expected outcome here, not a bug, so callers should treat
// it as a normal request-time error to surface to the client.
func (r *Router) Resolve(requestedModel string) (string, error) {
	if requestedModel == r.baseModel {
		return r.baseModel, nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.resident[requestedModel] {
		return requestedModel, nil
	}
	return "", fmt.Errorf("%w: %q", ErrAdapterNotLoaded, requestedModel)
}

// MarkLoaded records adapterName as resident and routable.
func (r *Router) MarkLoaded(adapterName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resident[adapterName] = true
}

// MarkUnloaded removes adapterName from the resident set; Resolve
// refuses it afterward.
func (r *Router) MarkUnloaded(adapterName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.resident, adapterName)
}
