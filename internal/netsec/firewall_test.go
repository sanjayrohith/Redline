package netsec_test

import (
	"strings"
	"testing"

	"github.com/sanjayrohith/redline/internal/netsec"
)

func TestBuildEgressRuleset_DropsAllRFC1918Ranges(t *testing.T) {
	ruleset := netsec.BuildEgressRuleset()

	for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
		want := "ip daddr " + cidr + " drop"
		if !strings.Contains(ruleset, want) {
			t.Errorf("ruleset missing rule %q; got:\n%s", want, ruleset)
		}
	}
}

func TestBuildEgressRuleset_DropsLinkLocalMetadataRange(t *testing.T) {
	ruleset := netsec.BuildEgressRuleset()
	want := "ip daddr 169.254.0.0/16 drop"
	if !strings.Contains(ruleset, want) {
		t.Errorf("ruleset missing rule %q; got:\n%s", want, ruleset)
	}
}

func TestBuildEgressRuleset_DefaultPolicyIsAccept(t *testing.T) {
	// Only RFC 1918 space is denied at this point; later steps narrow the
	// default further (link-local metadata block, then an allowlist).
	ruleset := netsec.BuildEgressRuleset()
	if !strings.Contains(ruleset, "policy accept;") {
		t.Errorf("ruleset does not declare an accept default policy; got:\n%s", ruleset)
	}
}
