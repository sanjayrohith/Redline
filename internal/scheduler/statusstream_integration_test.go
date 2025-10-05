package scheduler_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/nomad/api"

	"github.com/sanjayrohith/redline/internal/nomadclient"
	"github.com/sanjayrohith/redline/internal/scheduler"
)

func startTestAgent(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("nomad"); err != nil {
		t.Skip("skipping integration test: nomad binary not found on PATH")
	}

	httpPort := mustFreePort(t)
	rpcPort := mustFreePort(t)
	serfPort := mustFreePort(t)
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

	cmd := exec.Command("nomad", "agent", "-dev", "-config="+configPath) //nolint:gosec // fixed binary, local temp config
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

func mustFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

type fakeDeploymentStateUpdater struct {
	mu     sync.Mutex
	states map[string][]string
}

func newFakeDeploymentStateUpdater() *fakeDeploymentStateUpdater {
	return &fakeDeploymentStateUpdater{states: map[string][]string{}}
}

func (f *fakeDeploymentStateUpdater) UpdateState(_ context.Context, id, state string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[id] = append(f.states[id], state)
	return nil
}

func (f *fakeDeploymentStateUpdater) statesFor(id string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.states[id]...)
}

func rawExecInferenceJob(deploymentID string) *api.Job {
	jobID := nomadclient.InferenceJobID(deploymentID)
	job := api.NewServiceJob(jobID, jobID, "global", 50)
	job.Datacenters = []string{"dc1"}

	tg := api.NewTaskGroup("group", 1)
	restartAttempts := 0
	restartMode := "fail"
	tg.RestartPolicy = &api.RestartPolicy{Attempts: &restartAttempts, Mode: &restartMode}

	task := api.NewTask("task", "raw_exec")
	task.Config = map[string]any{"command": "/bin/sh", "args": []string{"-c", "sleep 30"}}
	cpu, mem := 50, 32
	task.Resources = &api.Resources{CPU: &cpu, MemoryMB: &mem}

	tg.AddTask(task)
	job.AddTaskGroup(tg)
	return job
}

func TestStatusStreamer_PersistsAllocationTransitions(t *testing.T) {
	addr := startTestAgent(t)

	client, err := nomadclient.NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	updater := newFakeDeploymentStateUpdater()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	streamer := scheduler.NewStatusStreamer(client, updater, logger)

	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = streamer.Run(streamCtx) }()

	// Give the stream a moment to establish before the job produces events.
	time.Sleep(500 * time.Millisecond)

	submitCtx, submitCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer submitCancel()

	const deploymentID = "dep-status-test"
	job := rawExecInferenceJob(deploymentID)
	if _, err := client.SubmitJob(submitCtx, job); err != nil {
		t.Fatalf("SubmitJob() error = %v", err)
	}
	t.Cleanup(func() { _, _ = client.StopJob(context.Background(), *job.ID, true) })

	deadline := time.Now().Add(20 * time.Second)
	var states []string
	for time.Now().Before(deadline) {
		states = updater.statesFor(deploymentID)
		if len(states) > 0 && states[len(states)-1] == "ready" {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}

	if len(states) == 0 {
		t.Fatal("no deployment state transitions were persisted")
	}
	if states[len(states)-1] != "ready" {
		t.Errorf("final state = %q, want ready; full sequence = %v", states[len(states)-1], states)
	}
}
