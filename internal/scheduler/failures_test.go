package scheduler

import (
	"testing"

	"github.com/hashicorp/nomad/api"
)

func allocWithEvent(taskName string, ev *api.TaskEvent, failed bool) *api.Allocation {
	return &api.Allocation{
		ID:           "alloc-1",
		ClientStatus: "failed",
		TaskStates: map[string]*api.TaskState{
			taskName: {Failed: failed, Events: []*api.TaskEvent{ev}},
		},
	}
}

func TestClassifyAllocationFailure_ImagePull(t *testing.T) {
	ev := &api.TaskEvent{Type: api.TaskDriverFailure, DisplayMessage: "Failed to pull image \"redline/vllm:bogus\": manifest unknown"}
	failure := ClassifyAllocationFailure(allocWithEvent("vllm", ev, true))

	if failure == nil || failure.Class != FailureImagePull {
		t.Fatalf("failure = %+v, want FailureImagePull", failure)
	}
	if failure.TaskName != "vllm" {
		t.Errorf("TaskName = %q, want vllm", failure.TaskName)
	}
}

func TestClassifyAllocationFailure_OOMKilledViaDetails(t *testing.T) {
	ev := &api.TaskEvent{Type: api.TaskTerminated, Details: map[string]string{"oom_killed": "true"}}
	failure := ClassifyAllocationFailure(allocWithEvent("vllm", ev, true))

	if failure == nil || failure.Class != FailureOOMKilled {
		t.Fatalf("failure = %+v, want FailureOOMKilled", failure)
	}
}

func TestClassifyAllocationFailure_OOMKilledViaMessage(t *testing.T) {
	ev := &api.TaskEvent{Type: api.TaskTerminated, DisplayMessage: "Task ran out of memory: OOM"}
	failure := ClassifyAllocationFailure(allocWithEvent("vllm", ev, true))

	if failure == nil || failure.Class != FailureOOMKilled {
		t.Fatalf("failure = %+v, want FailureOOMKilled", failure)
	}
}

func TestClassifyAllocationFailure_TaskCrash(t *testing.T) {
	ev := &api.TaskEvent{Type: api.TaskNotRestarting, DisplayMessage: "Exceeded restart attempts"}
	failure := ClassifyAllocationFailure(allocWithEvent("vllm", ev, true))

	if failure == nil || failure.Class != FailureTaskCrash {
		t.Fatalf("failure = %+v, want FailureTaskCrash", failure)
	}
}

func TestClassifyAllocationFailure_NilForHealthyAllocation(t *testing.T) {
	alloc := &api.Allocation{ID: "alloc-1", ClientStatus: "running"}
	if failure := ClassifyAllocationFailure(alloc); failure != nil {
		t.Errorf("failure = %+v, want nil for a running allocation", failure)
	}
}

func TestClassifyAllocationFailure_UnknownWhenUnclassifiable(t *testing.T) {
	alloc := &api.Allocation{ID: "alloc-1", ClientStatus: "failed"}
	failure := ClassifyAllocationFailure(alloc)

	if failure == nil || failure.Class != FailureUnknown {
		t.Fatalf("failure = %+v, want FailureUnknown", failure)
	}
}

func TestClassifyAllocationFailure_MostRecentEventWins(t *testing.T) {
	alloc := &api.Allocation{
		ID:           "alloc-1",
		ClientStatus: "failed",
		TaskStates: map[string]*api.TaskState{
			"vllm": {
				Failed: true,
				Events: []*api.TaskEvent{
					{Type: api.TaskReceived},
					{Type: api.TaskDriverFailure, DisplayMessage: "pull access denied"},
				},
			},
		},
	}

	failure := ClassifyAllocationFailure(alloc)
	if failure == nil || failure.Class != FailureImagePull {
		t.Fatalf("failure = %+v, want FailureImagePull (the most recent event)", failure)
	}
}

func TestClassifyEvaluationFailure_NodesExhausted(t *testing.T) {
	eval := &api.Evaluation{
		FailedTGAllocs: map[string]*api.AllocationMetric{
			"inference": {NodesEvaluated: 5, NodesExhausted: 5},
		},
	}

	failure := ClassifyEvaluationFailure(eval)
	if failure == nil || failure.Class != FailurePlacement {
		t.Fatalf("failure = %+v, want FailurePlacement", failure)
	}
	if failure.TaskName != "inference" {
		t.Errorf("TaskName = %q, want inference", failure.TaskName)
	}
}

func TestClassifyEvaluationFailure_NilWhenNoFailures(t *testing.T) {
	eval := &api.Evaluation{}
	if failure := ClassifyEvaluationFailure(eval); failure != nil {
		t.Errorf("failure = %+v, want nil", failure)
	}
}

func TestAllocationFailureError_Error(t *testing.T) {
	err := &AllocationFailureError{Class: FailureOOMKilled, AllocID: "alloc-1", Message: "killed"}
	want := "scheduler: allocation alloc-1 failed (oom_killed): killed"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
