package inference_test

import (
	"errors"
	"testing"

	"github.com/sanjayrohith/redline/internal/ingest"

	"github.com/sanjayrohith/redline/internal/inference"
)

func TestRenderEngineArgs_FP16NeedsNoQuantizationFlag(t *testing.T) {
	geometry := ingest.ModelGeometry{ContextLength: 4096}
	profile := inference.NodeHardwareProfile{GPUCount: 1, VRAMBytesPerGPU: 24 << 30}

	args, err := inference.RenderEngineArgs(geometry, 10<<30, inference.PrecisionFP16, profile)
	if err != nil {
		t.Fatalf("RenderEngineArgs() error = %v", err)
	}

	if args.DType != "float16" {
		t.Errorf("DType = %q, want float16", args.DType)
	}
	if args.Quantization != "" {
		t.Errorf("Quantization = %q, want empty for fp16", args.Quantization)
	}
	if args.TensorParallelSize != 1 {
		t.Errorf("TensorParallelSize = %d, want 1", args.TensorParallelSize)
	}
	if args.MaxModelLen != 4096 {
		t.Errorf("MaxModelLen = %d, want 4096", args.MaxModelLen)
	}
}

func TestRenderEngineArgs_FP8AndINT4CarryTheirQuantizationFlag(t *testing.T) {
	geometry := ingest.ModelGeometry{ContextLength: 2048}
	profile := inference.NodeHardwareProfile{GPUCount: 1, VRAMBytesPerGPU: 24 << 30}

	fp8, err := inference.RenderEngineArgs(geometry, 5<<30, inference.PrecisionFP8, profile)
	if err != nil {
		t.Fatalf("RenderEngineArgs(fp8) error = %v", err)
	}
	if fp8.Quantization != "fp8" {
		t.Errorf("fp8 Quantization = %q, want fp8", fp8.Quantization)
	}

	int4, err := inference.RenderEngineArgs(geometry, 3<<30, inference.PrecisionINT4, profile)
	if err != nil {
		t.Fatalf("RenderEngineArgs(int4) error = %v", err)
	}
	if int4.Quantization != "awq" {
		t.Errorf("int4 Quantization = %q, want awq", int4.Quantization)
	}
}

func TestRenderEngineArgs_PicksSmallestTensorParallelDegreeThatFits(t *testing.T) {
	geometry := ingest.ModelGeometry{ContextLength: 8192}
	profile := inference.NodeHardwareProfile{GPUCount: 4, VRAMBytesPerGPU: 20 << 30}

	// 70GiB of weights: one GPU (20GiB) isn't enough, two (40GiB) isn't
	// enough, three (60GiB) isn't enough, four (80GiB) covers it.
	args, err := inference.RenderEngineArgs(geometry, 70<<30, inference.PrecisionFP16, profile)
	if err != nil {
		t.Fatalf("RenderEngineArgs() error = %v", err)
	}
	if args.TensorParallelSize != 4 {
		t.Errorf("TensorParallelSize = %d, want 4", args.TensorParallelSize)
	}
}

func TestRenderEngineArgs_RefusesAModelThatDoesNotFitAtAnyDegree(t *testing.T) {
	geometry := ingest.ModelGeometry{ContextLength: 8192}
	profile := inference.NodeHardwareProfile{GPUCount: 2, VRAMBytesPerGPU: 20 << 30}

	_, err := inference.RenderEngineArgs(geometry, 100<<30, inference.PrecisionFP16, profile)
	if !errors.Is(err, inference.ErrModelExceedsNodeCapacity) {
		t.Fatalf("RenderEngineArgs() error = %v, want ErrModelExceedsNodeCapacity", err)
	}
}

func TestRenderEngineArgs_RejectsAnInvalidHardwareProfile(t *testing.T) {
	geometry := ingest.ModelGeometry{ContextLength: 4096}

	if _, err := inference.RenderEngineArgs(geometry, 1<<30, inference.PrecisionFP16, inference.NodeHardwareProfile{}); err == nil {
		t.Fatal("RenderEngineArgs() with a zero-value hardware profile error = nil, want an error")
	}
}
