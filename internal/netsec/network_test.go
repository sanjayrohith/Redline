package netsec_test

import (
	"testing"

	"github.com/sanjayrohith/redline/internal/netsec"
)

func TestAllocationNetwork_UsesDedicatedCNINetwork(t *testing.T) {
	net := netsec.AllocationNetwork()

	want := "cni/" + netsec.CNINetworkName
	if net.Mode != want {
		t.Errorf("AllocationNetwork().Mode = %q, want %q", net.Mode, want)
	}
}
