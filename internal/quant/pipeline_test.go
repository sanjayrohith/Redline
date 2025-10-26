package quant_test

import (
	"testing"

	"github.com/sanjayrohith/redline/internal/inference"
	"github.com/sanjayrohith/redline/internal/ingest"
	"github.com/sanjayrohith/redline/internal/quant"
)

func TestSelectPrecision_70BModelFitsOn80GBCardAtFP8WhenSupported(t *testing.T) {
	const paramCount = 70_000_000_000
	footprint := ingest.ComputeVRAMFootprint(paramCount, ingest.ModelGeometry{})
	const vramBytesPerGPU = 80 << 30 // one 80GB card

	got := quant.SelectPrecision(true, footprint.FP16.TotalBytes, footprint.FP8.TotalBytes, vramBytesPerGPU)

	if got != inference.PrecisionFP8 {
		t.Fatalf("SelectPrecision() = %v, want fp8 (fp16 needs ~140GB, doesn't fit; fp8 needs ~70GB, fits)", got)
	}
}

func TestSelectPrecision_StaysAtFP16WhenItAlreadyFits(t *testing.T) {
	const paramCount = 7_000_000_000 // a 7B model easily fits at fp16
	footprint := ingest.ComputeVRAMFootprint(paramCount, ingest.ModelGeometry{})
	const vramBytesPerGPU = 24 << 30

	got := quant.SelectPrecision(true, footprint.FP16.TotalBytes, footprint.FP8.TotalBytes, vramBytesPerGPU)

	if got != inference.PrecisionFP16 {
		t.Errorf("SelectPrecision() = %v, want fp16 when it already fits without quantizing", got)
	}
}

func TestSelectPrecision_NeverSelectsFP8WithoutHardwareSupport(t *testing.T) {
	const paramCount = 70_000_000_000
	footprint := ingest.ComputeVRAMFootprint(paramCount, ingest.ModelGeometry{})
	const vramBytesPerGPU = 80 << 30

	got := quant.SelectPrecision(false, footprint.FP16.TotalBytes, footprint.FP8.TotalBytes, vramBytesPerGPU)

	if got != inference.PrecisionFP16 {
		t.Errorf("SelectPrecision() = %v, want fp16 (the caller's tensor-parallel sizing handles the rest) when the node has no native FP8 support", got)
	}
}

func TestSelectPrecision_StaysAtFP16WhenEvenFP8DoesNotFit(t *testing.T) {
	const paramCount = 400_000_000_000 // far larger than one GPU at any supported precision here
	footprint := ingest.ComputeVRAMFootprint(paramCount, ingest.ModelGeometry{})
	const vramBytesPerGPU = 80 << 30

	got := quant.SelectPrecision(true, footprint.FP16.TotalBytes, footprint.FP8.TotalBytes, vramBytesPerGPU)

	if got != inference.PrecisionFP16 {
		t.Errorf("SelectPrecision() = %v, want fp16 (fall through to tensor-parallel sizing) when fp8 alone still doesn't fit a single GPU", got)
	}
}
