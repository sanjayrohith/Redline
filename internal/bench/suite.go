// Package bench runs a small, fixed suite of standardized evaluation
// prompts against a pinned backend at an explicit sampling configuration,
// scoring each by exact substring match against its expected answer. It
// is deliberately not a large benchmark corpus (MMLU-scale suites are a
// separate ingestion problem of their own) - it exists to exercise and
// prove out the benchmark dispatch, scoring, and comparison path with a
// suite that is genuinely graded, not a placeholder.
package bench

import (
	"context"
	"strings"

	"github.com/sanjayrohith/redline/internal/inference"
)

// Task is one graded prompt: Complete's response is scored correct if it
// contains ExpectedSubstring, case-insensitively.
type Task struct {
	Name              string `json:"name"`
	Prompt            string `json:"prompt"`
	ExpectedSubstring string `json:"expected_substring"`
}

// DefaultSuite is a small, fixed set of deterministic arithmetic and
// factual-recall prompts.
var DefaultSuite = []Task{
	{Name: "arithmetic-addition", Prompt: "What is 12 + 7? Answer with just the number.", ExpectedSubstring: "19"},
	{Name: "arithmetic-multiplication", Prompt: "What is 6 * 7? Answer with just the number.", ExpectedSubstring: "42"},
	{Name: "factual-capital", Prompt: "What is the capital of France? Answer with just the city name.", ExpectedSubstring: "paris"},
	{Name: "factual-ordering", Prompt: "Which is larger, 100 or 99? Answer with just the number.", ExpectedSubstring: "100"},
	{Name: "instruction-following", Prompt: "Repeat the word 'redline' exactly once.", ExpectedSubstring: "redline"},
}

// TaskResult is one task's graded outcome.
type TaskResult struct {
	Task     Task   `json:"task"`
	Response string `json:"response"`
	Passed   bool   `json:"passed"`
}

// Result is a full suite run's outcome.
type Result struct {
	Tasks       []TaskResult
	PassedCount int
	Score       float64 // PassedCount / len(Tasks), in [0, 1].
}

// SamplingConfig pins the deterministic parameters a benchmark run
// exercises the backend at.
type SamplingConfig struct {
	Model       string
	Temperature float64
	TopP        float64
	Seed        *int64
}

// Run dispatches every task in suite against backend using cfg, scoring
// each response and returning the aggregate Result. A per-task backend
// error is recorded as a failed (not errored) task, since a benchmark run
// must always finish with a complete, comparable Result rather than
// aborting partway through.
func Run(ctx context.Context, backend inference.Backend, suite []Task, cfg SamplingConfig) (*Result, error) {
	result := &Result{Tasks: make([]TaskResult, len(suite))}

	for i, task := range suite {
		req := inference.CompletionRequest{
			RequestID:   "bench-" + task.Name,
			Model:       cfg.Model,
			Messages:    []inference.Message{{Role: "user", Content: task.Prompt}},
			Temperature: cfg.Temperature,
			TopP:        cfg.TopP,
		}

		resp, err := backend.Complete(ctx, req)
		var response string
		var passed bool
		if err == nil {
			response = resp.Content
			passed = strings.Contains(strings.ToLower(response), strings.ToLower(task.ExpectedSubstring))
		}

		result.Tasks[i] = TaskResult{Task: task, Response: response, Passed: passed}
		if passed {
			result.PassedCount++
		}
	}

	if len(suite) > 0 {
		result.Score = float64(result.PassedCount) / float64(len(suite))
	}
	return result, nil
}
