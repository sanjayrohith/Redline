// Package netsec builds the per-allocation network isolation Nomad and its
// CNI plugin chain enforce: a dedicated network namespace per inference
// allocation, and an egress ruleset denying everything except the one
// destination an allocation legitimately needs.
package netsec

import "github.com/hashicorp/nomad/api"

// CNINetworkName is the CNI network Nomad wires every inference
// allocation's network namespace into (see deploy/cni/redline-alloc.conflist).
// Its bridge plugin gives each allocation its own namespace and IP off a
// private, non-NAT'd subnet; its chained firewall plugin is where the
// egress ruleset from BuildEgressRuleset is installed. Two allocations on
// the same node share the bridge but never route to one another: the
// firewall plugin's per-interface ruleset (see AllocationChain) denies
// bridge-to-bridge traffic by default, and neither allocation ever learns
// a sibling's address since Nomad assigns it, not DNS or service
// discovery reachable from inside the sandbox.
const CNINetworkName = "redline-alloc"

// AllocationNetwork returns the task group network stanza that places an
// inference allocation into its own CNI-managed namespace instead of
// sharing the host's network stack, which is Nomad's default for the
// docker driver absent an explicit network block.
func AllocationNetwork() *api.NetworkResource {
	return &api.NetworkResource{
		Mode: "cni/" + CNINetworkName,
	}
}
