package netsec_test

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestLateralMovement_SiblingAllocationCannotReachAnothersPort stands up
// two concurrently running sandbox instances - each `runsc do` invocation
// gets its own network namespace, mirroring two sibling allocations on
// the same redline-alloc CNI network each landing in their own
// per-allocation namespace - and asserts one cannot reach a port the
// other has bound and is actively serving on.
//
// The listening allocation's sandbox self-checks its own server (a curl
// from inside the *same* namespace) before the sibling probe runs, and
// only proceeds once that self-check reports success: that is the proof
// the server is actually live, so a refusal from the sibling's separate
// namespace can only mean network isolation, not merely an absent listener.
func TestLateralMovement_SiblingAllocationCannotReachAnothersPort(t *testing.T) {
	requireRunscCurlPython(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const port = "18765"
	serverScript := "python3 -m http.server " + port + " --bind 127.0.0.1 & " +
		"sleep 1; " +
		"curl -s -o /dev/null -w 'SELFCHECK=%{http_code}\\n' http://127.0.0.1:" + port + "/; " +
		"sleep 15"

	server := exec.CommandContext(ctx, "runsc", "-rootless", "-ignore-cgroups", "-network=none", "do",
		"sh", "-c", serverScript)
	stdout, err := server.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe(): %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("starting the listening allocation's sandbox: %v", err)
	}
	t.Cleanup(func() { _ = server.Process.Kill() })

	if !waitForSelfCheck(t, stdout, 10*time.Second) {
		t.Fatal("listening allocation never reported SELFCHECK=200 from inside its own namespace")
	}

	siblingOut, siblingErr := exec.CommandContext(ctx, "runsc", "-rootless", "-ignore-cgroups", "-network=none", "do",
		"curl", "-sv", "-m", "3", "http://127.0.0.1:"+port+"/").CombinedOutput()

	if siblingErr == nil {
		t.Fatalf("sibling sandbox reached the other allocation's port; output: %s", siblingOut)
	}
	if !strings.Contains(string(siblingOut), "refused") && !strings.Contains(string(siblingOut), "Could not connect") {
		t.Errorf("sibling probe failed for an unexpected reason (want connection refused, its own namespace has no such listener); output: %s", siblingOut)
	}
}

// waitForSelfCheck scans r for the listening sandbox's own confirmation
// that its server answered locally, up to timeout.
func waitForSelfCheck(t *testing.T, r interface{ Read([]byte) (int, error) }, timeout time.Duration) bool {
	t.Helper()
	done := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "SELFCHECK=200") {
				done <- true
				return
			}
		}
		done <- false
	}()

	select {
	case ok := <-done:
		return ok
	case <-time.After(timeout):
		return false
	}
}

func requireRunscCurlPython(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"runsc", "curl", "python3"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("skipping sandbox test: %s not found on PATH", bin)
		}
	}
}
