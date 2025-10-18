package sandbox_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/sandbox"
)

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("filepath.Abs(%q) error = %v", path, err)
	}
	return abs
}

func requireRunsc(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("runsc"); err != nil {
		t.Skip("skipping sandbox test: runsc binary not found on PATH")
	}
}

// TestRunsc_ReportsGVisorKernelSignature asserts a process running inside
// the sandbox observes gVisor's synthetic kernel identity, not the host
// kernel's - the most basic proof that syscalls are being intercepted by
// gVisor's own application kernel rather than passed straight through.
func TestRunsc_ReportsGVisorKernelSignature(t *testing.T) {
	requireRunsc(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := sandbox.Do(ctx, "uname", "-s", "-r", "-v")
	if err != nil {
		t.Fatalf("sandbox.Do() error = %v", err)
	}

	if !regexp.MustCompile(`(?i)gvisor`).MatchString(out) {
		t.Errorf("uname output = %q, want it to contain gVisor's kernel signature", out)
	}
}

// TestRunsc_RejectsSyscallOutsidePermittedSet compares init_module(2)'s
// failure mode on the bare host against inside the sandbox. On the host,
// an unprivileged caller gets EPERM: the syscall exists, permission is
// denied. Inside gVisor, there is no kernel module concept in the sentry
// at all, so the same call fails with ENOSYS - a categorically different
// failure that can only come from the syscall genuinely not being
// implemented, not merely being permission-checked.
func TestRunsc_RejectsSyscallOutsidePermittedSet(t *testing.T) {
	requireRunsc(t)
	requirePython3(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hostOut, err := exec.CommandContext(ctx, "python3", "testdata/probe_init_module.py").CombinedOutput() // #nosec G204 -- fixed test script path
	if err != nil {
		t.Fatalf("host probe error = %v (output: %s)", err, hostOut)
	}
	if !strings.Contains(string(hostOut), "errno=1 ") {
		t.Fatalf("host probe output = %q, want errno=1 (EPERM) on the bare host", hostOut)
	}

	sandboxOut, err := sandbox.Do(ctx, "python3", mustAbs(t, "testdata/probe_init_module.py"))
	if err != nil {
		t.Fatalf("sandbox.Do() error = %v", err)
	}
	if !strings.Contains(sandboxOut, "errno=38 ") {
		t.Errorf("sandbox probe output = %q, want errno=38 (ENOSYS): the syscall must not be implemented inside gVisor", sandboxOut)
	}
}

func requirePython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("skipping sandbox test: python3 not found on PATH")
	}
}
