package inference

import (
	"errors"
	"fmt"

	"github.com/sanjayrohith/redline/internal/ingest"
)

// Precision is the weight quantization a deployment is configured to
// serve a model at - one of the three variants ingest.ComputeVRAMFootprint
// estimates.
type Precision string

// Supported precisions, matching ingest.ComputeVRAMFootprint's variants.
const (
	PrecisionFP16 Precision = "fp16"
	PrecisionFP8  Precision = "fp8"
	PrecisionINT4 Precision = "int4"
)

// NodeHardwareProfile describes the GPUs on the worker node a deployment
// is being placed onto.
type NodeHardwareProfile struct {
	GPUCount        int
	VRAMBytesPerGPU int64
}

// EngineArgs are the vLLM engine arguments Redline derives per
// deployment, rather than hand-configuring per model:
// https://docs.vllm.ai/en/latest/serving/engine_args.html.
type EngineArgs struct {
	// MaxModelLen is --max-model-len: the context length vLLM allocates
	// KV cache space for.
	MaxModelLen int
	// DType is --dtype.
	DType string
	// Quantization is --quantization, empty when the weights need no
	// quantization flag (fp16 runs at native precision).
	Quantization string
	// TensorParallelSize is --tensor-parallel-size: how many of the
	// node's GPUs the model's weights are sharded across.
	TensorParallelSize int
	// GPUMemoryUtilization is --gpu-memory-utilization, always within
	// [MinGPUMemoryUtilization, MaxGPUMemoryUtilization] regardless of
	// what was requested - see ClampGPUMemoryUtilization.
	GPUMemoryUtilization float64
}

// MinGPUMemoryUtilization and MaxGPUMemoryUtilization bound
// --gpu-memory-utilization. Values above the ceiling reliably trigger
// host RAM exhaustion during CUDA graph compilation - the compiler stages
// graph buffers in host memory sized relative to how much device memory
// it believes it can use, and a ceiling near 100% starves that staging
// step on any node without an unusually large amount of host RAM
// relative to its VRAM. Values below the floor are needlessly wasteful.
const (
	MinGPUMemoryUtilization = 0.80
	MaxGPUMemoryUtilization = 0.85
)

// ClampGPUMemoryUtilization bounds requested to
// [MinGPUMemoryUtilization, MaxGPUMemoryUtilization], regardless of what
// a caller (or a model's own recommended config) asked for.
func ClampGPUMemoryUtilization(requested float64) float64 {
	switch {
	case requested < MinGPUMemoryUtilization:
		return MinGPUMemoryUtilization
	case requested > MaxGPUMemoryUtilization:
		return MaxGPUMemoryUtilization
	default:
		return requested
	}
}

// ErrModelExceedsNodeCapacity means the model's weights, sharded across
// every GPU the node has, still do not fit - no tensor-parallel degree up
// to profile.GPUCount has enough aggregate VRAM.
var ErrModelExceedsNodeCapacity = errors.New("inference: model does not fit on this node at any tensor-parallel degree")

// dtypeForPrecision maps a Precision to vLLM's --dtype and --quantization
// flags. fp16 is native precision (no quantization flag); fp8 and int4
// keep the engine's compute dtype at half while quantizing weights,
// matching how vLLM itself expects these to be configured.
func dtypeForPrecision(p Precision) (dtype, quantization string, err error) {
	switch p {
	case PrecisionFP16:
		return "float16", "", nil
	case PrecisionFP8:
		return "float16", "fp8", nil
	case PrecisionINT4:
		return "float16", "awq", nil
	default:
		return "", "", fmt.Errorf("inference: unknown precision %q", p)
	}
}

// RenderEngineArgs derives a deployment's vLLM engine arguments from the
// model's parsed geometry, its VRAM footprint at the requested precision,
// and the target node's hardware profile.
//
// The tensor-parallel degree is the smallest value in [1, profile.GPUCount]
// whose aggregate VRAM (degree * profile.VRAMBytesPerGPU) covers
// footprintBytes: sharding is real overhead (activation memory, all-reduce
// traffic), so the model is spread across only as many GPUs as it actually
// needs, not the whole node by default.
func RenderEngineArgs(geometry ingest.ModelGeometry, footprintBytes int64, precision Precision, profile NodeHardwareProfile, requestedGPUMemoryUtilization float64) (EngineArgs, error) {
	dtype, quantization, err := dtypeForPrecision(precision)
	if err != nil {
		return EngineArgs{}, err
	}

	if profile.GPUCount <= 0 || profile.VRAMBytesPerGPU <= 0 {
		return EngineArgs{}, fmt.Errorf("inference: invalid node hardware profile %+v", profile)
	}

	tp := 0
	for degree := 1; degree <= profile.GPUCount; degree++ {
		if int64(degree)*profile.VRAMBytesPerGPU >= footprintBytes {
			tp = degree
			break
		}
	}
	if tp == 0 {
		return EngineArgs{}, fmt.Errorf("%w: needs %d bytes, node has %d GPU(s) with %d bytes each",
			ErrModelExceedsNodeCapacity, footprintBytes, profile.GPUCount, profile.VRAMBytesPerGPU)
	}

	return EngineArgs{
		MaxModelLen:          geometry.ContextLength,
		DType:                dtype,
		Quantization:         quantization,
		TensorParallelSize:   tp,
		GPUMemoryUtilization: ClampGPUMemoryUtilization(requestedGPUMemoryUtilization),
	}, nil
}
