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

func TestBuildInferenceJob_GPUDeviceConstraint(t *testing.T) {
	spec := InferenceJobSpec{
		DeploymentID: "dep-123",
		Image:        "redline/vllm:latest",
		GPUModel:     "A100",
		MinVRAMBytes: 40 * 1024 * 1024 * 1024, // 40 GiB
	}

	job := BuildInferenceJob(spec)
	devices := job.TaskGroups[0].Tasks[0].Resources.Devices
	if len(devices) != 1 {
		t.Fatalf("len(Devices) = %d, want 1", len(devices))
	}

	device := devices[0]
	if device.Name != "nvidia/gpu/A100" {
		t.Errorf("Name = %q, want nvidia/gpu/A100", device.Name)
	}
	if device.Count == nil || *device.Count != 1 {
		t.Errorf("Count = %v, want 1", device.Count)
	}
	if len(device.Constraints) != 1 {
		t.Fatalf("len(Constraints) = %d, want 1", len(device.Constraints))
	}
	c := device.Constraints[0]
	if c.LTarget != "${device.attr.memory}" || c.Operand != ">=" || c.RTarget != "40 GiB" {
		t.Errorf("constraint = %+v, want memory >= 40 GiB", c)
	}
}

func TestBuildInferenceJob_GenericGPUWhenModelUnspecified(t *testing.T) {
	job := BuildInferenceJob(InferenceJobSpec{DeploymentID: "dep-1", Image: "img"})
	device := job.TaskGroups[0].Tasks[0].Resources.Devices[0]

	if device.Name != "nvidia/gpu" {
		t.Errorf("Name = %q, want nvidia/gpu", device.Name)
	}
	if len(device.Constraints) != 0 {
		t.Errorf("Constraints = %+v, want none when MinVRAMBytes is 0", device.Constraints)
	}
}

func TestFormatGiB_RoundsUp(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{40 * 1024 * 1024 * 1024, "40 GiB"},
		{40*1024*1024*1024 + 1, "41 GiB"}, // one byte over rounds up
		{1, "1 GiB"},
	}
	for _, tt := range tests {
		if got := formatGiB(tt.bytes); got != tt.want {
			t.Errorf("formatGiB(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestBuildInferenceJob_DefaultResourceLimits(t *testing.T) {
	job := BuildInferenceJob(InferenceJobSpec{DeploymentID: "dep-1", Image: "img"})
	res := job.TaskGroups[0].Tasks[0].Resources

	if res.CPU == nil || *res.CPU != DefaultCPUMHz {
		t.Errorf("CPU = %v, want %d", res.CPU, DefaultCPUMHz)
	}
	if res.MemoryMB == nil || *res.MemoryMB != DefaultMemoryMB {
		t.Errorf("MemoryMB = %v, want %d", res.MemoryMB, DefaultMemoryMB)
	}
	if res.MemoryMaxMB == nil || *res.MemoryMaxMB != DefaultMemoryMB {
		t.Errorf("MemoryMaxMB = %v, want %d (hard cap defaults to the target with no burst headroom)", res.MemoryMaxMB, DefaultMemoryMB)
	}
}

func TestBuildInferenceJob_ExplicitResourceLimits(t *testing.T) {
	spec := InferenceJobSpec{
		DeploymentID: "dep-1",
		Image:        "img",
		CPUMHz:       4000,
		MemoryMB:     8192,
		MemoryMaxMB:  16384,
	}
	res := BuildInferenceJob(spec).TaskGroups[0].Tasks[0].Resources

	if *res.CPU != 4000 {
		t.Errorf("CPU = %d, want 4000", *res.CPU)
	}
	if *res.MemoryMB != 8192 {
		t.Errorf("MemoryMB = %d, want 8192", *res.MemoryMB)
	}
	if *res.MemoryMaxMB != 16384 {
		t.Errorf("MemoryMaxMB = %d, want 16384 (an explicit hard ceiling above the target)", *res.MemoryMaxMB)
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
