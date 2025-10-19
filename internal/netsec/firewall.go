package netsec

import "strings"

// rfc1918Ranges are the private address blocks reserved by RFC 1918. A
// compromised inference container has no legitimate reason to originate
// traffic into any of them: they are where internal services (the
// database, the queue, the control plane API, sibling nodes) live.
var rfc1918Ranges = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
}

// linkLocalRange is RFC 3927 address space. Cloud providers serve
// instance identity documents and, on many platforms, ambient IAM/role
// credentials from a well-known link-local address inside this range -
// a container that can reach it can mint credentials for the node it
// happens to be scheduled on. There is no legitimate reason for an
// inference container to originate traffic here at all.
const linkLocalRange = "169.254.0.0/16"

// BuildEgressRuleset renders the nftables ruleset installed into an
// allocation's network namespace by the redline-alloc CNI network's
// chained firewall plugin (see deploy/cni/redline-alloc.conflist). It
// denies outbound traffic to RFC 1918 address space by default, so a
// compromised model container cannot scan or reach any internal service
// reachable from the node's bridge.
func BuildEgressRuleset() string {
	var b strings.Builder
	b.WriteString("table inet redline_egress {\n")
	b.WriteString("  chain egress {\n")
	b.WriteString("    type filter hook output priority 0; policy accept;\n")
	for _, cidr := range rfc1918Ranges {
		b.WriteString("    ip daddr " + cidr + " drop\n")
	}
	b.WriteString("    ip daddr " + linkLocalRange + " drop\n")
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}
