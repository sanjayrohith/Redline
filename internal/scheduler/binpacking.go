package scheduler

import (
	"errors"
	"fmt"
)

// NodeCapacity is one worker node's current free VRAM, as of whatever
// snapshot the caller supplies.
type NodeCapacity struct {
	NodeID        string
	FreeVRAMBytes int64
}

// ErrNoCapacity means no node has enough free VRAM for the requested
// footprint.
var ErrNoCapacity = errors.New("scheduler: no node has sufficient free VRAM")

// SelectNode picks the best-fit node for a model needing requiredBytes of
// VRAM: the node with the least free VRAM that still fits the request,
// minimizing wasted capacity on every other node. It refuses placement -
// returning ErrNoCapacity rather than picking an undersized node - when
// no node has enough room, since placing on an undersized node would
// only trade a scheduling failure now for an out-of-memory crash later.
func SelectNode(nodes []NodeCapacity, requiredBytes int64) (*NodeCapacity, error) {
	var best *NodeCapacity
	var largestAvailable int64

	for i := range nodes {
		if nodes[i].FreeVRAMBytes > largestAvailable {
			largestAvailable = nodes[i].FreeVRAMBytes
		}
		if nodes[i].FreeVRAMBytes < requiredBytes {
			continue
		}
		if best == nil || nodes[i].FreeVRAMBytes < best.FreeVRAMBytes {
			best = &nodes[i]
		}
	}

	if best == nil {
		return nil, fmt.Errorf("%w: need %d bytes free, largest available is %d bytes across %d node(s)",
			ErrNoCapacity, requiredBytes, largestAvailable, len(nodes))
	}

	return best, nil
}
