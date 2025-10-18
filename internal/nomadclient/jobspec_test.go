package nomadclient

import (
	"testing"
	"time"
)

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
	if task.Config["runtime"] != DefaultRuntime {
		t.Errorf("Config[runtime] = %v, want %q (gVisor by default)", task.Config["runtime"], DefaultRuntime)
	}
	if task.Env["MODEL"] != "meta-llama/Llama-3-8B" {
		t.Errorf("Env[MODEL] = %q, want meta-llama/Llama-3-8B", task.Env["MODEL"])
	}
}

func TestBuildInferenceJob_SetsNvproxyEnv(t *testing.T) {
	job := BuildInferenceJob(InferenceJobSpec{
		DeploymentID: "dep-1", Image: "img",
		Env: map[string]string{"MODEL": "meta-llama/Llama-3-8B"},
	})
	env := job.TaskGroups[0].Tasks[0].Env

	if env["NVIDIA_VISIBLE_DEVICES"] != "all" {
		t.Errorf("Env[NVIDIA_VISIBLE_DEVICES] = %q, want all", env["NVIDIA_VISIBLE_DEVICES"])
	}
	if env["NVIDIA_DRIVER_CAPABILITIES"] != "compute,utility" {
		t.Errorf("Env[NVIDIA_DRIVER_CAPABILITIES] = %q, want compute,utility", env["NVIDIA_DRIVER_CAPABILITIES"])
	}
	if env["MODEL"] != "meta-llama/Llama-3-8B" {
		t.Errorf("Env[MODEL] = %q, want meta-llama/Llama-3-8B (caller env preserved alongside the sandbox baseline)", env["MODEL"])
	}
}

func TestBuildInferenceJob_CallerEnvOverridesNvproxyDefaults(t *testing.T) {
	job := BuildInferenceJob(InferenceJobSpec{
		DeploymentID: "dep-1", Image: "img",
		Env: map[string]string{"NVIDIA_VISIBLE_DEVICES": "0,1"},
	})
	env := job.TaskGroups[0].Tasks[0].Env

	if env["NVIDIA_VISIBLE_DEVICES"] != "0,1" {
		t.Errorf("Env[NVIDIA_VISIBLE_DEVICES] = %q, want 0,1 (caller override should win)", env["NVIDIA_VISIBLE_DEVICES"])
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

func TestBuildInferenceJob_DefaultDrainAndKillTimeout(t *testing.T) {
	job := BuildInferenceJob(InferenceJobSpec{DeploymentID: "dep-1", Image: "img"})
	tg := job.TaskGroups[0]

	if tg.ShutdownDelay == nil || *tg.ShutdownDelay != DefaultDrainPeriod {
		t.Errorf("ShutdownDelay = %v, want %v", tg.ShutdownDelay, DefaultDrainPeriod)
	}

	task := tg.Tasks[0]
	if task.KillTimeout == nil || *task.KillTimeout != DefaultKillTimeout {
		t.Errorf("KillTimeout = %v, want %v", task.KillTimeout, DefaultKillTimeout)
	}
	if task.KillSignal != "SIGTERM" {
		t.Errorf("KillSignal = %q, want SIGTERM", task.KillSignal)
	}
}

func TestBuildInferenceJob_ExplicitDrainAndKillTimeout(t *testing.T) {
	spec := InferenceJobSpec{
		DeploymentID: "dep-1",
		Image:        "img",
		DrainPeriod:  20 * time.Second,
		KillTimeout:  90 * time.Second,
	}
	job := BuildInferenceJob(spec)

	if *job.TaskGroups[0].ShutdownDelay != 20*time.Second {
		t.Errorf("ShutdownDelay = %v, want 20s", *job.TaskGroups[0].ShutdownDelay)
	}
	if *job.TaskGroups[0].Tasks[0].KillTimeout != 90*time.Second {
		t.Errorf("KillTimeout = %v, want 90s", *job.TaskGroups[0].Tasks[0].KillTimeout)
	}
}

func TestBuildInferenceJob_ExplicitRuntimeOverride(t *testing.T) {
	job := BuildInferenceJob(InferenceJobSpec{DeploymentID: "dep-1", Image: "img", Runtime: "runc"})
	if got := job.TaskGroups[0].Tasks[0].Config["runtime"]; got != "runc" {
		t.Errorf("Config[runtime] = %v, want runc", got)
	}
}

func TestBuildInferenceJob_ReadOnlyRootfsWithScopedTmpfs(t *testing.T) {
	job := BuildInferenceJob(InferenceJobSpec{DeploymentID: "dep-1", Image: "img"})
	cfg := job.TaskGroups[0].Tasks[0].Config

	if cfg["readonly_rootfs"] != true {
		t.Errorf("Config[readonly_rootfs] = %v, want true", cfg["readonly_rootfs"])
	}

	mounts, ok := cfg["mounts"].([]map[string]any)
	if !ok || len(mounts) != 2 {
		t.Fatalf("Config[mounts] = %v, want 2 tmpfs mounts", cfg["mounts"])
	}

	byTarget := map[string]map[string]any{}
	for _, m := range mounts {
		byTarget[m["target"].(string)] = m
	}

	tmp, ok := byTarget["/tmp"]
	if !ok || tmp["type"] != "tmpfs" {
		t.Errorf("/tmp mount = %+v, want type tmpfs", tmp)
	}
	if size := tmp["tmpfs_options"].(map[string]any)["size"]; size != int64(DefaultTmpfsSizeBytes) {
		t.Errorf("/tmp size = %v, want %d", size, DefaultTmpfsSizeBytes)
	}

	shm, ok := byTarget["/dev/shm"]
	if !ok || shm["type"] != "tmpfs" {
		t.Errorf("/dev/shm mount = %+v, want type tmpfs", shm)
	}
	if size := shm["tmpfs_options"].(map[string]any)["size"]; size != int64(DefaultShmSizeBytes) {
		t.Errorf("/dev/shm size = %v, want %d", size, DefaultShmSizeBytes)
	}
}

func TestBuildInferenceJob_ExplicitTmpfsAndShmSizes(t *testing.T) {
	job := BuildInferenceJob(InferenceJobSpec{
		DeploymentID: "dep-1", Image: "img",
		TmpfsSizeBytes: 256 * 1024 * 1024,
		ShmSizeBytes:   2 * 1024 * 1024 * 1024,
	})
	mounts := job.TaskGroups[0].Tasks[0].Config["mounts"].([]map[string]any)

	for _, m := range mounts {
		size := m["tmpfs_options"].(map[string]any)["size"]
		switch m["target"] {
		case "/tmp":
			if size != int64(256*1024*1024) {
				t.Errorf("/tmp size = %v, want 256MiB", size)
			}
		case "/dev/shm":
			if size != int64(2*1024*1024*1024) {
				t.Errorf("/dev/shm size = %v, want 2GiB", size)
			}
		}
	}
}

func TestBuildInferenceJob_DropsAllCapabilitiesAddsNone(t *testing.T) {
	job := BuildInferenceJob(InferenceJobSpec{DeploymentID: "dep-1", Image: "img"})
	cfg := job.TaskGroups[0].Tasks[0].Config

	capDrop, ok := cfg["cap_drop"].([]string)
	if !ok || len(capDrop) != 1 || capDrop[0] != "ALL" {
		t.Errorf("Config[cap_drop] = %v, want [ALL]", cfg["cap_drop"])
	}

	capAdd, ok := cfg["cap_add"].([]string)
	if !ok || len(capAdd) != 0 {
		t.Errorf("Config[cap_add] = %v, want an empty slice", cfg["cap_add"])
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
