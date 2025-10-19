package sandbox_test

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/sandbox"
)

// driverVersionPattern matches nvproxy's own log line reporting the host
// NVIDIA driver version it detected and is proxying against.
var driverVersionPattern = regexp.MustCompile(`NVIDIA driver version: (\S+)`)

// TestRunsc_NVProxyDetectsHostGPUDriver asserts nvproxy - gVisor's NVIDIA
// driver proxy - independently identifies the exact driver version
// `nvidia-smi` reports on the host, proving the sandbox's GPU support
// negotiates against the real installed driver rather than a stub.
//
// This does not assert GPU enumeration or CUDA kernel execution *inside*
// the sandbox: `runsc do`'s simplified single-command path has no
// equivalent of the OCI device injection nvidia-container-toolkit performs
// for a full `docker run --gpus`, so /dev/nvidia* is never present inside
// a `runsc do` sandbox regardless of nvproxy being enabled - confirmed by
// running `ls /dev/nvidia*` inside the sandbox during development, which
// reports every device node absent even with nvproxy on. That device
// passthrough is production-only, provisioned by
// deploy/provisioning/docker-daemon.json's runsc runtime registration
// together with the nvidia-container-toolkit hook, and requires a live
// Nomad/Docker stack with root-level daemon configuration to exercise -
// out of scope for this package's non-invasive, rootless verification.
// nvproxy's driver handshake happens before any device node would be
// opened, so it remains real, independent signal on its own.
func TestRunsc_NVProxyDetectsHostGPUDriver(t *testing.T) {
	requireRunsc(t)
	requireNVIDIASMI(t)

	hostOut, err := exec.Command("nvidia-smi", "--query-gpu=driver_version", "--format=csv,noheader").CombinedOutput()
	if err != nil {
		t.Skipf("skipping: nvidia-smi present but not functional: %v (%s)", err, hostOut)
	}
	hostVersion := strings.TrimSpace(string(hostOut))
	if hostVersion == "" {
		t.Skip("skipping: nvidia-smi reported no driver version")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// `true` is a no-op: nvproxy's driver handshake runs during sandbox
	// boot, before the sandboxed command even executes.
	out, err := sandbox.DoWithNVProxyDebug(ctx, "true")
	if err != nil {
		t.Fatalf("sandbox.DoWithNVProxyDebug() error = %v", err)
	}

	match := driverVersionPattern.FindStringSubmatch(out)
	if match == nil {
		t.Fatalf("nvproxy debug output did not report a driver version; output: %s", out)
	}
	if match[1] != hostVersion {
		t.Errorf("nvproxy detected driver version %q, want it to match the host's %q", match[1], hostVersion)
	}
}

func requireNVIDIASMI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		t.Skip("skipping GPU sandbox test: nvidia-smi not found on PATH")
	}
}
