package netsec_test

import (
	"strings"
	"testing"

	"github.com/sanjayrohith/redline/internal/netsec"
)

func testConfig() netsec.EgressConfig {
	return netsec.EgressConfig{ArtifactCacheAddr: "10.4.0.15/32", ArtifactCachePort: 9000}
}

func TestBuildEgressRuleset_DropsAllRFC1918Ranges(t *testing.T) {
	ruleset := netsec.BuildEgressRuleset(testConfig())

	for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
		want := "ip daddr " + cidr + " drop"
		if !strings.Contains(ruleset, want) {
			t.Errorf("ruleset missing rule %q; got:\n%s", want, ruleset)
		}
	}
}

func TestBuildEgressRuleset_DropsLinkLocalMetadataRange(t *testing.T) {
	ruleset := netsec.BuildEgressRuleset(testConfig())
	want := "ip daddr 169.254.0.0/16 drop"
	if !strings.Contains(ruleset, want) {
		t.Errorf("ruleset missing rule %q; got:\n%s", want, ruleset)
	}
}

func TestBuildEgressRuleset_DefaultPolicyIsDrop(t *testing.T) {
	ruleset := netsec.BuildEgressRuleset(testConfig())
	if !strings.Contains(ruleset, "policy drop;") {
		t.Errorf("ruleset does not declare a drop default policy; got:\n%s", ruleset)
	}
}

func TestBuildEgressRuleset_AllowsOnlyTheArtifactCache(t *testing.T) {
	ruleset := netsec.BuildEgressRuleset(testConfig())
	want := "ip daddr 10.4.0.15/32 tcp dport 9000 accept"
	if !strings.Contains(ruleset, want) {
		t.Errorf("ruleset missing rule %q; got:\n%s", want, ruleset)
	}
	if strings.Count(ruleset, "accept") != 1 {
		t.Errorf("ruleset has more than one accept rule; got:\n%s", ruleset)
	}
}

func TestBuildEgressRuleset_OmitsAcceptRuleWhenUnconfigured(t *testing.T) {
	ruleset := netsec.BuildEgressRuleset(netsec.EgressConfig{})
	if strings.Contains(ruleset, "accept") {
		t.Errorf("ruleset should have no accept rule with an empty config; got:\n%s", ruleset)
	}
}
