// Package sandbox provides a thin wrapper around invoking runsc directly
// (bypassing Docker/containerd) for node-level verification that the
// gVisor sandbox actually behaves as configured - not just that it is
// configured.
package sandbox

import (
	"context"
	"fmt"
	"os/exec"
)

// Do runs command inside a gVisor sandbox via `runsc do`, rootless and
// without touching host cgroups or networking, and returns its combined
// output. This mirrors the isolation posture of the "runsc" runtime
// handler registered in containerd (see deploy/containerd/runsc.toml)
// closely enough to verify sandbox behavior directly, without requiring
// a running Nomad/Docker/containerd stack.
func Do(ctx context.Context, command ...string) (string, error) {
	args := append([]string{"-rootless", "-ignore-cgroups", "-network=none", "do"}, command...)
	cmd := exec.CommandContext(ctx, "runsc", args...) // #nosec G204 -- args are compile-time literals from callers in this package's own tests
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("sandbox: runsc do %v: %w (output: %s)", command, err, out)
	}
	return string(out), nil
}
