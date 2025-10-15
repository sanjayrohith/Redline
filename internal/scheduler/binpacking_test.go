package scheduler

import (
	"errors"
	"testing"
)

func TestSelectNode_BestFitMinimizesWaste(t *testing.T) {
	nodes := []NodeCapacity{
		{NodeID: "huge", FreeVRAMBytes: 80 * 1024 * 1024 * 1024},
		{NodeID: "tight-fit", FreeVRAMBytes: 24 * 1024 * 1024 * 1024},
		{NodeID: "too-small", FreeVRAMBytes: 8 * 1024 * 1024 * 1024},
	}

	got, err := SelectNode(nodes, 16*1024*1024*1024)
	if err != nil {
		t.Fatalf("SelectNode() error = %v", err)
	}
	if got.NodeID != "tight-fit" {
		t.Errorf("NodeID = %q, want tight-fit (smallest node that still fits)", got.NodeID)
	}
}

func TestSelectNode_RefusesPlacementWhenNoneFit(t *testing.T) {
	nodes := []NodeCapacity{
		{NodeID: "a", FreeVRAMBytes: 8 * 1024 * 1024 * 1024},
		{NodeID: "b", FreeVRAMBytes: 12 * 1024 * 1024 * 1024},
	}

	_, err := SelectNode(nodes, 40*1024*1024*1024)
	if !errors.Is(err, ErrNoCapacity) {
		t.Errorf("error = %v, want ErrNoCapacity", err)
	}
}

func TestSelectNode_ExactFitIsAllowed(t *testing.T) {
	nodes := []NodeCapacity{{NodeID: "exact", FreeVRAMBytes: 16 * 1024 * 1024 * 1024}}

	got, err := SelectNode(nodes, 16*1024*1024*1024)
	if err != nil {
		t.Fatalf("SelectNode() error = %v", err)
	}
	if got.NodeID != "exact" {
		t.Errorf("NodeID = %q, want exact", got.NodeID)
	}
}

func TestSelectNode_EmptyNodeList(t *testing.T) {
	if _, err := SelectNode(nil, 1024); !errors.Is(err, ErrNoCapacity) {
		t.Errorf("error = %v, want ErrNoCapacity", err)
	}
}

func TestSelectNode_NeverPicksUndersizedNode(t *testing.T) {
	// Every node is undersized: SelectNode must never return one of them,
	// even the closest, since that would risk an OOM crash rather than a
	// clean scheduling failure.
	nodes := []NodeCapacity{
		{NodeID: "a", FreeVRAMBytes: 10},
		{NodeID: "b", FreeVRAMBytes: 11}, // closest to the requirement, still short
	}

	if _, err := SelectNode(nodes, 12); !errors.Is(err, ErrNoCapacity) {
		t.Errorf("error = %v, want ErrNoCapacity", err)
	}
}
