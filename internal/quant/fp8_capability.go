// Package quant detects hardware quantization support and drives the
// quantization pipeline models are ingested through.
package quant

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// FP8MinComputeCapability is the lowest CUDA compute capability with
// native FP8 tensor core support: Ada Lovelace (8.9) and Hopper (9.0)
// silicon and later. Ampere (8.6 - this repository's own development and
// test GPU included) and earlier have no FP8 execution units at all;
// running FP8-quantized weights there would mean emulating FP8 math
// through fp16, which defeats the memory and throughput point of
// quantizing in the first place.
const FP8MinComputeCapability = 8.9

// DetectFP8Support parses a compute capability string as reported by
// `nvidia-smi --query-gpu=compute_cap --format=csv,noheader` (e.g. "8.9",
// "8.6") and reports whether that value indicates native FP8 tensor core
// support.
func DetectFP8Support(computeCapability string) (bool, error) {
	cc, err := strconv.ParseFloat(strings.TrimSpace(computeCapability), 64)
	if err != nil {
		return false, fmt.Errorf("quant: parse compute capability %q: %w", computeCapability, err)
	}
	return cc >= FP8MinComputeCapability, nil
}

// ProbeNodeFP8Support runs nvidia-smi on the local node to determine its
// GPU's compute capability, then reports whether it has native FP8
// support - for recording once, at node registration, rather than
// re-probed on every placement decision.
func ProbeNodeFP8Support(ctx context.Context) (bool, error) {
	out, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=compute_cap", "--format=csv,noheader").Output() // #nosec G204 -- fixed arguments, no user input
	if err != nil {
		return false, fmt.Errorf("quant: probe compute capability: %w", err)
	}

	// A node with multiple GPUs reports one line per device; take the
	// first non-empty line as representative. Mixed-capability nodes are
	// out of scope - Redline's own provisioning keeps a node's GPUs
	// homogeneous.
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		return DetectFP8Support(line)
	}
	return false, fmt.Errorf("quant: nvidia-smi returned no compute capability")
}
