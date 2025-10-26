package quant_test

import (
	"errors"
	"testing"

	"github.com/sanjayrohith/redline/internal/inference"
	"github.com/sanjayrohith/redline/internal/ingest"
	"github.com/sanjayrohith/redline/internal/quant"
	"github.com/sanjayrohith/redline/internal/scheduler"
)

func TestSelectPlacement_PrefersFP16WhenItAlreadyFits(t *testing.T) {
	footprint := ingest.ComputeVRAMFootprint(7_000_000_000, ingest.ModelGeometry{})
	nodes := []scheduler.NodeCapacity{{NodeID: "node-1", FreeVRAMBytes: 24 << 30}}

	decision, err := quant.SelectPlacement(true, footprint, nodes)
	if err != nil {
		t.Fatalf("SelectPlacement() error = %v", err)
	}
	if decision.Precision != inference.PrecisionFP16 {
		t.Errorf("Precision = %v, want fp16", decision.Precision)
	}
	if decision.Node.NodeID != "node-1" {
		t.Errorf("Node.NodeID = %q, want node-1", decision.Node.NodeID)
	}
}

func TestSelectPlacement_FallsBackToFP8WhenNoNodeFitsFP16(t *testing.T) {
	// 70B model: ~140GB at fp16 (fits nowhere here), ~70GB at fp8.
	footprint := ingest.ComputeVRAMFootprint(70_000_000_000, ingest.ModelGeometry{})
	nodes := []scheduler.NodeCapacity{{NodeID: "node-1", FreeVRAMBytes: 80 << 30}}

	decision, err := quant.SelectPlacement(true, footprint, nodes)
	if err != nil {
		t.Fatalf("SelectPlacement() error = %v", err)
	}
	if decision.Precision != inference.PrecisionFP8 {
		t.Errorf("Precision = %v, want fp8 (fp16 doesn't fit any node, fp8 does)", decision.Precision)
	}
	if decision.Node.NodeID != "node-1" {
		t.Errorf("Node.NodeID = %q, want node-1", decision.Node.NodeID)
	}
}

func TestSelectPlacement_QuantizationIncreasesAchievableDensity(t *testing.T) {
	// The same node pool, the same model: without fp8 it doesn't place;
	// with fp8 available it does. This is the density improvement
	// quantization is supposed to deliver at the scheduler level.
	footprint := ingest.ComputeVRAMFootprint(70_000_000_000, ingest.ModelGeometry{})
	nodes := []scheduler.NodeCapacity{{NodeID: "node-1", FreeVRAMBytes: 80 << 30}}

	if _, err := quant.SelectPlacement(false, footprint, nodes); !errors.Is(err, scheduler.ErrNoCapacity) {
		t.Fatalf("SelectPlacement(fp8Supported=false) error = %v, want ErrNoCapacity", err)
	}

	decision, err := quant.SelectPlacement(true, footprint, nodes)
	if err != nil {
		t.Fatalf("SelectPlacement(fp8Supported=true) error = %v", err)
	}
	if decision.Precision != inference.PrecisionFP8 {
		t.Errorf("Precision = %v, want fp8", decision.Precision)
	}
}

func TestSelectPlacement_NoNodeFitsEitherPrecision(t *testing.T) {
	footprint := ingest.ComputeVRAMFootprint(400_000_000_000, ingest.ModelGeometry{})
	nodes := []scheduler.NodeCapacity{{NodeID: "node-1", FreeVRAMBytes: 80 << 30}}

	_, err := quant.SelectPlacement(true, footprint, nodes)
	if !errors.Is(err, scheduler.ErrNoCapacity) {
		t.Fatalf("SelectPlacement() error = %v, want ErrNoCapacity", err)
	}
}
