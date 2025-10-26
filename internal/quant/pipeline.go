package quant

import "github.com/sanjayrohith/redline/internal/inference"

// SelectPrecision picks the precision a deployment serves at: FP16 by
// default, dropping to FP8 dynamic quantization only when both hold -
// the node's hardware actually has native FP8 support (see
// DetectFP8Support/ProbeNodeFP8Support), and the model's FP16 footprint
// does not already fit on a single GPU. Quantizing a model that already
// fits gains nothing and costs accuracy for no reason; on hardware
// without FP8 tensor cores, "quantizing" would mean emulating FP8 math
// through fp16, which is slower with no memory benefit, so it is never
// selected there regardless of whether the model needs the space.
//
// The end effect this pipeline is built for: a 70B model that needs
// ~140GB at FP16 - too large for a single 80GB card even before
// considering activation memory - drops to ~70GB at FP8 on FP8-capable
// silicon, fitting on that single card without tensor parallelism at all.
func SelectPrecision(fp8Supported bool, fp16Bytes, fp8Bytes, vramBytesPerGPU int64) inference.Precision {
	if fp16Bytes <= vramBytesPerGPU {
		return inference.PrecisionFP16
	}
	if fp8Supported && fp8Bytes <= vramBytesPerGPU {
		return inference.PrecisionFP8
	}
	return inference.PrecisionFP16
}
