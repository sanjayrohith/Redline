package nomadclient

import "testing"

func TestBuildInferenceJob(t *testing.T) {
	spec := InferenceJobSpec{
		DeploymentID: "dep-123",
		Image:        "redline/vllm:latest",
		Env:          map[string]string{"MODEL": "meta-llama/Llama-3-8B"},
	}

	job := BuildInferenceJob(spec)

	if *job.ID != "inference-dep-123" {
		t.Errorf("ID = %q, want inference-dep-123", *job.ID)
	}
	if *job.Type != "service" {
		t.Errorf("Type = %q, want service", *job.Type)
	}
	if len(job.TaskGroups) != 1 {
		t.Fatalf("len(TaskGroups) = %d, want 1", len(job.TaskGroups))
	}

	tg := job.TaskGroups[0]
	if tg.RestartPolicy == nil || *tg.RestartPolicy.Attempts != 3 {
		t.Errorf("RestartPolicy = %+v, want Attempts=3", tg.RestartPolicy)
	}
	if *tg.RestartPolicy.Mode != "fail" {
		t.Errorf("RestartPolicy.Mode = %q, want fail", *tg.RestartPolicy.Mode)
	}

	if len(tg.Tasks) != 1 {
		t.Fatalf("len(Tasks) = %d, want 1", len(tg.Tasks))
	}
	task := tg.Tasks[0]
	if task.Driver != "docker" {
		t.Errorf("Driver = %q, want docker", task.Driver)
	}
	if task.Config["image"] != spec.Image {
		t.Errorf("Config[image] = %v, want %q", task.Config["image"], spec.Image)
	}
	if task.Env["MODEL"] != "meta-llama/Llama-3-8B" {
		t.Errorf("Env[MODEL] = %q, want meta-llama/Llama-3-8B", task.Env["MODEL"])
	}
}

func TestInferenceJobID_IsDeterministic(t *testing.T) {
	if got := InferenceJobID("dep-123"); got != "inference-dep-123" {
		t.Errorf("InferenceJobID() = %q, want inference-dep-123", got)
	}
	if got := InferenceJobID("dep-456"); got != "inference-dep-456" {
		t.Errorf("InferenceJobID() = %q, want inference-dep-456", got)
	}
}
