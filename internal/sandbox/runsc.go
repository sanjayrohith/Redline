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
	return run(ctx, nil, command)
}

// DoWithNVProxyDebug runs command inside a gVisor sandbox with nvproxy (the
// sandbox's NVIDIA driver proxy) enabled and its debug log mirrored to the
// returned output, so a caller can observe what nvproxy detected about the
// host GPU even when the command itself has no way to report that.
//
// This does not grant the sandboxed process access to the GPU device
// nodes: `runsc do`'s simplified single-command path has no equivalent of
// the OCI device injection nvidia-container-toolkit performs for a full
// `docker run --gpus`/Nomad-managed container, so /dev/nvidia* is absent
// inside the sandbox here. nvproxy's own driver handshake - which happens
// before any device node is opened - is still real and independently
// observable in the debug log.
func DoWithNVProxyDebug(ctx context.Context, command ...string) (string, error) {
	return run(ctx, []string{"-nvproxy", "-debug", "-alsologtostderr"}, command)
}

func run(ctx context.Context, extraFlags []string, command []string) (string, error) {
	args := append([]string{"-rootless", "-ignore-cgroups", "-network=none"}, extraFlags...)
	args = append(args, "do")
	args = append(args, command...)
	cmd := exec.CommandContext(ctx, "runsc", args...) // #nosec G204 -- args are compile-time literals from callers in this package's own tests
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("sandbox: runsc do %v: %w (output: %s)", command, err, out)
	}
	return string(out), nil
}
