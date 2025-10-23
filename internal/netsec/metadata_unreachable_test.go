package netsec_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/sandbox"
)

// TestMetadataService_UnreachableFromSandbox asserts a process inside the
// sandbox cannot establish a connection to the link-local metadata
// address this platform's egress ruleset denies (see
// BuildEgressRuleset's link-local rule).
//
// The failure mode asserted here is connection establishment failing
// immediately, not a hang or a routed response: gVisor's own network
// isolation (an allocation's sandbox has no host network access at all,
// only its dedicated CNI namespace) means the attempt never reaches a
// point where a peer could refuse or accept it - it fails at the local
// network stack with "network unreachable" before a single packet
// leaves the sandbox. That is a strictly stronger guarantee than a
// firewall-level REJECT (which implies routing succeeded and a rule
// intercepted the packet): there is no route to intercept in the first
// place. What must never happen is the connection succeeding, or the
// attempt hanging until the test's own timeout - either would mean the
// metadata address was, in fact, reachable in some form.
func TestMetadataService_UnreachableFromSandbox(t *testing.T) {
	requireRunscAndCurl(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	out, err := sandbox.Do(ctx, "curl", "-sv", "-m", "5", "http://169.254.169.254/latest/meta-data/")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("curl succeeded reaching the metadata address from inside the sandbox; output: %s", out)
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("curl took %s (>= its own 5s timeout): the attempt hung rather than failing immediately; output: %s", elapsed, out)
	}
	if !strings.Contains(out, "unreachable") && !strings.Contains(out, "Could not connect") {
		t.Errorf("curl failed for an unexpected reason (want an immediate unreachable/could-not-connect failure); output: %s", out)
	}
}

func requireRunscAndCurl(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("runsc"); err != nil {
		t.Skip("skipping sandbox test: runsc binary not found on PATH")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("skipping sandbox test: curl not found on PATH")
	}
}
