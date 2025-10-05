package scheduler

import (
	"fmt"
	"strings"

	"github.com/hashicorp/nomad/api"
)

// FailureClass distinguishes why a deployment failed, so the retry policy
// and the UI can respond differently to each cause: a placement failure
// might be retried on a different node, an image pull failure needs a
// human to fix the reference, an OOM kill needs a smaller footprint or
// bigger node, and a plain task crash might just be transient.
type FailureClass string

const (
	// FailurePlacement means Nomad could not find an eligible node.
	FailurePlacement FailureClass = "placement_failure"
	// FailureImagePull means the task driver could not pull the image.
	FailureImagePull FailureClass = "image_pull_failure"
	// FailureOOMKilled means the task exceeded its memory ceiling.
	FailureOOMKilled FailureClass = "oom_killed"
	// FailureTaskCrash means the task exited or stopped restarting for
	// any other reason.
	FailureTaskCrash FailureClass = "task_crash"
	// FailureUnknown means the allocation failed but no event matched a
	// known classification.
	FailureUnknown FailureClass = "unknown"
)

// AllocationFailureError is a typed, classified allocation or placement
// failure.
type AllocationFailureError struct {
	Class    FailureClass
	AllocID  string
	TaskName string
	Message  string
}

func (e *AllocationFailureError) Error() string {
	return fmt.Sprintf("scheduler: allocation %s failed (%s): %s", e.AllocID, e.Class, e.Message)
}

// ClassifyAllocationFailure inspects alloc's task events (most recent
// first) and returns a typed failure describing the first classifiable
// cause, or nil if the allocation is not in a failed state.
func ClassifyAllocationFailure(alloc *api.Allocation) *AllocationFailureError {
	if alloc == nil {
		return nil
	}

	for taskName, ts := range alloc.TaskStates {
		if ts == nil {
			continue
		}
		for i := len(ts.Events) - 1; i >= 0; i-- {
			if failure := classifyTaskEvent(alloc.ID, taskName, ts.Events[i]); failure != nil {
				return failure
			}
		}
	}

	if alloc.ClientStatus == "failed" {
		return &AllocationFailureError{
			Class:   FailureUnknown,
			AllocID: alloc.ID,
			Message: "allocation failed with no classifiable task event",
		}
	}

	return nil
}

func classifyTaskEvent(allocID, taskName string, ev *api.TaskEvent) *AllocationFailureError {
	if ev == nil {
		return nil
	}

	switch {
	case ev.Type == api.TaskDriverFailure && looksLikeImagePullFailure(ev):
		return &AllocationFailureError{Class: FailureImagePull, AllocID: allocID, TaskName: taskName, Message: ev.DisplayMessage}
	case isOOMEvent(ev):
		return &AllocationFailureError{Class: FailureOOMKilled, AllocID: allocID, TaskName: taskName, Message: ev.DisplayMessage}
	case ev.Type == api.TaskNotRestarting || (ev.Type == api.TaskTerminated && ev.Details["exit_code"] != "0"):
		return &AllocationFailureError{Class: FailureTaskCrash, AllocID: allocID, TaskName: taskName, Message: ev.DisplayMessage}
	default:
		return nil
	}
}

func looksLikeImagePullFailure(ev *api.TaskEvent) bool {
	msg := strings.ToLower(ev.DisplayMessage + " " + ev.Message)
	return strings.Contains(msg, "pull") ||
		strings.Contains(msg, "manifest unknown") ||
		strings.Contains(msg, "no such image")
}

func isOOMEvent(ev *api.TaskEvent) bool {
	if v, ok := ev.Details["oom_killed"]; ok && v == "true" {
		return true
	}
	msg := strings.ToLower(ev.DisplayMessage + " " + ev.Message)
	return strings.Contains(msg, "oom") || strings.Contains(msg, "out of memory")
}

// ClassifyEvaluationFailure inspects eval's per-task-group placement
// failures and returns a typed FailurePlacement error summarizing the
// first one, or nil if placement succeeded.
func ClassifyEvaluationFailure(eval *api.Evaluation) *AllocationFailureError {
	if eval == nil || len(eval.FailedTGAllocs) == 0 {
		return nil
	}

	for taskGroup, metric := range eval.FailedTGAllocs {
		return &AllocationFailureError{
			Class:    FailurePlacement,
			TaskName: taskGroup,
			Message:  summarizePlacementFailure(metric),
		}
	}

	return nil
}

func summarizePlacementFailure(metric *api.AllocationMetric) string {
	if metric == nil {
		return "placement failed for an unknown reason"
	}

	if metric.NodesExhausted > 0 {
		return fmt.Sprintf("%d of %d evaluated nodes were resource-exhausted", metric.NodesExhausted, metric.NodesEvaluated)
	}
	if len(metric.DimensionExhausted) > 0 {
		for dim, count := range metric.DimensionExhausted {
			return fmt.Sprintf("%d nodes exhausted on dimension %q", count, dim)
		}
	}
	if len(metric.ConstraintFiltered) > 0 {
		for constraint, count := range metric.ConstraintFiltered {
			return fmt.Sprintf("%d nodes filtered by constraint %q", count, constraint)
		}
	}

	return fmt.Sprintf("no eligible node found among %d evaluated", metric.NodesEvaluated)
}
