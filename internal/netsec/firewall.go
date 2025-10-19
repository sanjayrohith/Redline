package netsec

import (
	"fmt"
	"strings"
)

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

// EgressConfig parameterizes BuildEgressRuleset with the one destination
// an inference allocation is legitimately allowed to reach.
type EgressConfig struct {
	// ArtifactCacheAddr is the internal MinIO endpoint's resolved
	// address, as "ip/32:port" - nftables matches addresses, not
	// hostnames, so this must already be resolved by whatever renders
	// the ruleset at provisioning time, not looked up by nft itself.
	ArtifactCacheAddr string
	// ArtifactCachePort is the TCP port ArtifactCacheAddr listens on.
	ArtifactCachePort int
}

// BuildEgressRuleset renders the nftables ruleset installed into an
// allocation's network namespace by the redline-alloc CNI network's
// chained firewall plugin (see deploy/cni/redline-alloc.conflist).
//
// The default policy is drop: an inference container has exactly one
// legitimate egress destination, the internal artifact cache it pulls
// model weights from, so everything else - the rest of the private
// address space, the link-local metadata range, and the public internet
// alike - is denied by default rather than enumerated as an exception.
// The RFC 1918 and link-local rules stay explicit rather than folding
// into the default-drop policy: they document *why* those ranges in
// particular are unreachable (internal services, credential harvesting)
// even though, with the default already set to drop, they are redundant
// with it in practice.
func BuildEgressRuleset(cfg EgressConfig) string {
	var b strings.Builder
	b.WriteString("table inet redline_egress {\n")
	b.WriteString("  chain egress {\n")
	b.WriteString("    type filter hook output priority 0; policy drop;\n")
	if cfg.ArtifactCacheAddr != "" && cfg.ArtifactCachePort > 0 {
		fmt.Fprintf(&b, "    ip daddr %s tcp dport %d accept\n", cfg.ArtifactCacheAddr, cfg.ArtifactCachePort)
	}
	for _, cidr := range rfc1918Ranges {
		b.WriteString("    ip daddr " + cidr + " drop\n")
	}
	b.WriteString("    ip daddr " + linkLocalRange + " drop\n")
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}
