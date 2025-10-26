package quant

import (
	"fmt"

	"github.com/sanjayrohith/redline/internal/inference"
	"github.com/sanjayrohith/redline/internal/ingest"
	"github.com/sanjayrohith/redline/internal/scheduler"
)

// PlacementDecision is the outcome of SelectPlacement: which precision a
// model landed at, and which node its weights - at that precision - fit on.
type PlacementDecision struct {
	Precision inference.Precision
	Node      scheduler.NodeCapacity
}

// SelectPlacement feeds a model's VRAM footprint into bin-packing
// (scheduler.SelectNode) at FP16 first, and only recomputes placement
// against the FP8-quantized footprint if no node has room for FP16 -
// this is the whole point of quantization from the scheduler's side:
// it does not change bin-packing's decision procedure, it changes what
// footprint gets fed into it, so a node that could never hold this
// model's FP16 weights can now hold its FP8 weights instead. Preferring
// FP16 first when it already fits keeps quantization reserved for
// where headroom is the actual binding constraint, matching
// quant.SelectPrecision's own preference.
//
// fp8Supported is caller-supplied for the whole candidate pool rather
// than probed per node here: nodes are provisioned with homogeneous GPU
// hardware (see quant.ProbeNodeFP8Support's own doc comment), so a
// single supported/unsupported flag for one placement attempt is
// consistent with that, and keeps this function's shape matched to
// scheduler.SelectNode's own node-agnostic capacity check.
func SelectPlacement(fp8Supported bool, footprint ingest.VRAMFootprint, nodes []scheduler.NodeCapacity) (PlacementDecision, error) {
	if node, err := scheduler.SelectNode(nodes, footprint.FP16.TotalBytes); err == nil {
		return PlacementDecision{Precision: inference.PrecisionFP16, Node: *node}, nil
	}

	if !fp8Supported {
		return PlacementDecision{}, fmt.Errorf("quant: no node fits the fp16 footprint, and fp8 quantization is unavailable: %w", scheduler.ErrNoCapacity)
	}

	node, err := scheduler.SelectNode(nodes, footprint.FP8.TotalBytes)
	if err != nil {
		return PlacementDecision{}, fmt.Errorf("quant: no node fits even the fp8-quantized footprint: %w", err)
	}
	return PlacementDecision{Precision: inference.PrecisionFP8, Node: *node}, nil
}
