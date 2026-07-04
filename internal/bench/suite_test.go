package bench_test

import (
	"context"
	"testing"

	"github.com/sanjayrohith/redline/internal/bench"
	"github.com/sanjayrohith/redline/internal/inference"
)

type fakeBackend struct {
	responses map[string]string
	err       error
}

func (f *fakeBackend) Complete(_ context.Context, req inference.CompletionRequest) (*inference.CompletionResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &inference.CompletionResponse{Content: f.responses[req.RequestID]}, nil
}

func (f *fakeBackend) Stream(context.Context, inference.CompletionRequest) (<-chan inference.StreamEvent, error) {
	return nil, nil
}

func (f *fakeBackend) Cancel(context.Context, string) error { return nil }

func TestRun_ScoresEachTaskBySubstringMatch(t *testing.T) {
	suite := []bench.Task{
		{Name: "add", Prompt: "1+1?", ExpectedSubstring: "2"},
		{Name: "capital", Prompt: "capital of france?", ExpectedSubstring: "paris"},
	}
	backend := &fakeBackend{responses: map[string]string{
		"bench-add":     "The answer is 2.",
		"bench-capital": "It's Berlin.",
	}}

	result, err := bench.Run(context.Background(), backend, suite, bench.SamplingConfig{Model: "mock", Temperature: 0.2, TopP: 0.9})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.PassedCount != 1 {
		t.Errorf("PassedCount = %d, want 1", result.PassedCount)
	}
	if result.Score != 0.5 {
		t.Errorf("Score = %f, want 0.5", result.Score)
	}
	if !result.Tasks[0].Passed {
		t.Error("Tasks[0] (add) should have passed")
	}
	if result.Tasks[1].Passed {
		t.Error("Tasks[1] (capital) should have failed")
	}
}

func TestRun_BackendErrorCountsAsFailedNotAborted(t *testing.T) {
	suite := []bench.Task{{Name: "add", Prompt: "1+1?", ExpectedSubstring: "2"}}
	backend := &fakeBackend{err: context.DeadlineExceeded}

	result, err := bench.Run(context.Background(), backend, suite, bench.SamplingConfig{Model: "mock"})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil (a per-task error should not abort the suite)", err)
	}
	if result.PassedCount != 0 || len(result.Tasks) != 1 {
		t.Errorf("result = %+v, want one failed task", result)
	}
}

func TestRun_EmptySuiteScoresZeroWithoutDivideByZero(t *testing.T) {
	result, err := bench.Run(context.Background(), &fakeBackend{}, nil, bench.SamplingConfig{Model: "mock"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Score != 0 {
		t.Errorf("Score = %f, want 0 for an empty suite", result.Score)
	}
}
