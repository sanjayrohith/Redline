package nomadclient

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/nomad/api"
)

func trivialJob(jobID string) *api.Job {
	job := api.NewServiceJob(jobID, jobID, "global", 50)
	job.Datacenters = []string{"dc1"}

	tg := api.NewTaskGroup("group", 1)
	tg.RestartPolicy = &api.RestartPolicy{
		Attempts: intPtr(0),
		Mode:     stringPtr("fail"),
	}

	task := api.NewTask("task", "raw_exec")
	task.Config = map[string]any{
		"command": "/bin/sh",
		"args":    []string{"-c", "sleep 30"},
	}
	task.Resources = &api.Resources{CPU: intPtr(50), MemoryMB: intPtr(32)}

	tg.AddTask(task)
	job.AddTaskGroup(tg)

	return job
}

func TestClient_SubmitJobAllocationsAndStop(t *testing.T) {
	addr := startTestAgent(t)

	client, err := NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	job := trivialJob("nomadclient-test-job")

	evalID, err := client.SubmitJob(ctx, job)
	if err != nil {
		t.Fatalf("SubmitJob() error = %v", err)
	}
	if evalID == "" {
		t.Fatal("SubmitJob() returned an empty eval ID")
	}

	var allocs []*api.AllocationListStub
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		allocs, err = client.AllocationsForJob(ctx, *job.ID)
		if err != nil {
			t.Fatalf("AllocationsForJob() error = %v", err)
		}
		if len(allocs) > 0 {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if len(allocs) == 0 {
		t.Fatal("job never produced an allocation")
	}

	alloc, err := client.Allocation(ctx, allocs[0].ID)
	if err != nil {
		t.Fatalf("Allocation() error = %v", err)
	}
	if alloc.JobID != *job.ID {
		t.Errorf("alloc.JobID = %q, want %q", alloc.JobID, *job.ID)
	}

	stopEvalID, err := client.StopJob(ctx, *job.ID, true)
	if err != nil {
		t.Fatalf("StopJob() error = %v", err)
	}
	if stopEvalID == "" {
		t.Error("StopJob() returned an empty eval ID")
	}
}

func TestClient_AllocationNotFound(t *testing.T) {
	addr := startTestAgent(t)

	client, err := NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := client.Allocation(ctx, "00000000-0000-0000-0000-000000000000"); err == nil {
		t.Fatal("Allocation() error = nil, want error for an unknown allocation id")
	}
}

func TestClient_StopAllocation(t *testing.T) {
	addr := startTestAgent(t)

	client, err := NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	job := trivialJob("nomadclient-stop-alloc-job")
	if _, err := client.SubmitJob(ctx, job); err != nil {
		t.Fatalf("SubmitJob() error = %v", err)
	}
	t.Cleanup(func() { _, _ = client.StopJob(context.Background(), *job.ID, true) })

	var allocID string
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		allocs, err := client.AllocationsForJob(ctx, *job.ID)
		if err != nil {
			t.Fatalf("AllocationsForJob() error = %v", err)
		}
		if len(allocs) > 0 {
			allocID = allocs[0].ID
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if allocID == "" {
		t.Fatal("job never produced an allocation")
	}

	if err := client.StopAllocation(ctx, allocID); err != nil {
		t.Fatalf("StopAllocation() error = %v", err)
	}

	deadline = time.Now().Add(15 * time.Second)
	var desiredStatus string
	for time.Now().Before(deadline) {
		alloc, err := client.Allocation(ctx, allocID)
		if err != nil {
			t.Fatalf("Allocation() error = %v", err)
		}
		desiredStatus = alloc.DesiredStatus
		if desiredStatus == "stop" {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if desiredStatus != "stop" {
		t.Errorf("DesiredStatus = %q, want stop", desiredStatus)
	}
}
