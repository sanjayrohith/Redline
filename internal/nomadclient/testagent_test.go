package nomadclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// startTestAgent launches a real `nomad agent -dev` bound to an ephemeral
// port, waits for its HTTP API to become ready, and returns its address.
// The agent is killed and its data directory removed on test cleanup.
func startTestAgent(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("nomad"); err != nil {
		t.Skip("skipping integration test: nomad binary not found on PATH")
	}

	httpPort, err := freePort()
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	rpcPort, err := freePort()
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	serfPort, err := freePort()
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", httpPort)

	dataDir := t.TempDir()

	configPath := filepath.Join(t.TempDir(), "agent.hcl")
	configHCL := fmt.Sprintf(`
bind_addr = "127.0.0.1"
data_dir  = %q
ports {
  http = %d
  rpc  = %d
  serf = %d
}
`, dataDir, httpPort, rpcPort, serfPort)
	if err := os.WriteFile(configPath, []byte(configHCL), 0o600); err != nil {
		t.Fatalf("write agent config: %v", err)
	}

	cmd := exec.Command("nomad", "agent", "-dev", "-config="+configPath) //nolint:gosec // fixed binary name, config path is a local temp file this test wrote
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		t.Skipf("skipping integration test: could not start nomad agent: %v", err)
	}

	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	url := "http://" + addr + "/v1/agent/health"
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return "http://" + addr
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	t.Fatal("nomad dev agent never became healthy")
	return ""
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}
