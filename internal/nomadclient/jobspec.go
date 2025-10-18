package nomadclient

import (
	"fmt"
	"time"

	"github.com/hashicorp/nomad/api"
)

// InferenceJobSpec is the input to BuildInferenceJob: everything specific
// to one deployment, layered onto the shared base spec.
type InferenceJobSpec struct {
	DeploymentID string
	Image        string
	Env          map[string]string

	// GPUModel names the required NVIDIA GPU model (e.g. "A100", "H100").
	// Empty requests any NVIDIA GPU.
	GPUModel string
	// MinVRAMBytes is the minimum per-device memory the scheduler must
	// find before placing this job. Zero omits the constraint.
	MinVRAMBytes int64

	// CPUMHz is the task's CPU share in MHz. Defaults to
	// DefaultCPUMHz if <= 0.
	CPUMHz int
	// MemoryMB is the task's target memory in MB. Defaults to
	// DefaultMemoryMB if <= 0.
	MemoryMB int
	// MemoryMaxMB is the hard memory ceiling in MB: the task is OOM
	// killed if it exceeds this, rather than starving its node's other
	// allocations. Defaults to MemoryMB (no burst headroom) if <= 0.
	MemoryMaxMB int

	// DrainPeriod is how long Nomad waits after deregistering the
	// allocation before sending its kill signal, giving in-flight
	// requests routed via service discovery time to finish. Defaults to
	// DefaultDrainPeriod if <= 0.
	DrainPeriod time.Duration
	// KillTimeout is the grace period between the kill signal and a
	// forced SIGKILL. Defaults to DefaultKillTimeout if <= 0.
	KillTimeout time.Duration

	// Runtime is the Docker runtime handler the container executes
	// under. Defaults to DefaultRuntime ("runsc", gVisor's application
	// kernel) if empty - every model container is treated as hostile by
	// default, never the host kernel, unless a caller deliberately opts
	// out for a non-model task.
	Runtime string

	// TmpfsSizeBytes bounds the writable /tmp mount. Defaults to
	// DefaultTmpfsSizeBytes if <= 0.
	TmpfsSizeBytes int64
	// ShmSizeBytes bounds the writable /dev/shm mount, which PyTorch's
	// multiprocessing and NCCL both require for inter-process tensor
	// transfer. Defaults to DefaultShmSizeBytes if <= 0.
	ShmSizeBytes int64
}

// DefaultCPUMHz and DefaultMemoryMB are conservative defaults for the
// control-plane side of an inference task - the GPU does the real work;
// these bound the CPU-side driver process.
const (
	DefaultCPUMHz   = 2000
	DefaultMemoryMB = 4096
)

// DefaultDrainPeriod and DefaultKillTimeout bound graceful teardown: long
// enough for an in-flight generation to finish and for the allocation to
// drop out of service discovery, short enough that a stuck process is
// still reclaimed promptly.
const (
	DefaultDrainPeriod = 10 * time.Second
	DefaultKillTimeout = 30 * time.Second
)

// DefaultTmpfsSizeBytes and DefaultShmSizeBytes bound the only two
// writable paths a container gets, since the root filesystem itself is
// mounted read-only. 1GiB of shared memory is generous headroom for
// PyTorch/NCCL's tensor IPC even under tensor parallelism.
const (
	DefaultTmpfsSizeBytes = 512 * 1024 * 1024
	DefaultShmSizeBytes   = 1024 * 1024 * 1024
)

// DefaultRuntime is gVisor's application kernel: every inference
// container runs under it unless a spec explicitly overrides Runtime.
// The node's Docker daemon must have this runtime handler registered
// (see deploy/provisioning/docker-daemon.json) for the value to resolve
// to anything.
const DefaultRuntime = "runsc"

// BuildInferenceJob declares the base job specification for a stateless
// inference worker: docker task driver, the given image and environment,
// and a bounded restart policy so a crash-looping container does not
// retry forever.
func BuildInferenceJob(spec InferenceJobSpec) *api.Job {
	jobID := InferenceJobID(spec.DeploymentID)

	job := api.NewServiceJob(jobID, jobID, "global", 50)
	job.Datacenters = []string{"dc1"}
	job.Type = stringPtr("service")

	tg := api.NewTaskGroup("inference", 1)
	tg.RestartPolicy = &api.RestartPolicy{
		Attempts: intPtr(3),
		Interval: durationPtr(5 * time.Minute),
		Delay:    durationPtr(15 * time.Second),
		Mode:     stringPtr("fail"),
	}
	tg.ShutdownDelay = durationPtr(orDefault(spec.DrainPeriod, DefaultDrainPeriod))

	task := api.NewTask("vllm", "docker")
	runtime := spec.Runtime
	if runtime == "" {
		runtime = DefaultRuntime
	}
	tmpfsSize := spec.TmpfsSizeBytes
	if tmpfsSize <= 0 {
		tmpfsSize = DefaultTmpfsSizeBytes
	}
	shmSize := spec.ShmSizeBytes
	if shmSize <= 0 {
		shmSize = DefaultShmSizeBytes
	}

	task.Config = map[string]any{
		"image":           spec.Image,
		"runtime":         runtime,
		"readonly_rootfs": true,
		// Every Linux capability is dropped and none are added back:
		// there is no path from inside this container to raw sockets,
		// mount operations, or kernel module loading, regardless of
		// what the sandboxed process ends up doing.
		"cap_drop": []string{"ALL"},
		"cap_add":  []string{},
		// Hard cgroup ceilings, not soft shares: cpu_hard_limit turns
		// Resources.CPU into a real CFS quota the kernel enforces even
		// when the node has spare cycles, and MemoryMaxMB (set below via
		// task.Resources) is already a hard OOM-kill ceiling rather than
		// a reclaimable target. An infinite generation loop or a runaway
		// allocation hits a wall at the sandbox boundary instead of
		// starving every other allocation on the node.
		"cpu_hard_limit": true,
		"mounts": []map[string]any{
			{
				"type":   "tmpfs",
				"target": "/tmp",
				"tmpfs_options": map[string]any{
					"size": tmpfsSize,
				},
			},
			{
				"type":   "tmpfs",
				"target": "/dev/shm",
				"tmpfs_options": map[string]any{
					"size": shmSize,
				},
			},
		},
	}
	task.Env = mergeEnv(nvproxyEnv(), spec.Env)
	task.Resources = resourceRequest(spec)
	task.KillSignal = "SIGTERM"
	task.KillTimeout = durationPtr(orDefault(spec.KillTimeout, DefaultKillTimeout))

	tg.AddTask(task)
	job.AddTaskGroup(tg)

	return job
}

// InferenceJobID derives the deterministic Nomad job ID for a deployment,
// so submission, lookup, and stop all agree on the same identifier.
func InferenceJobID(deploymentID string) string {
	return fmt.Sprintf("inference-%s", deploymentID)
}

// resourceRequest builds the task's full resource block: explicit CPU
// shares, a target memory with a hard ceiling, and the GPU device
// constraint, so a runaway process is bounded rather than free to starve
// its node's other allocations.
func resourceRequest(spec InferenceJobSpec) *api.Resources {
	cpu := spec.CPUMHz
	if cpu <= 0 {
		cpu = DefaultCPUMHz
	}

	memory := spec.MemoryMB
	if memory <= 0 {
		memory = DefaultMemoryMB
	}

	memoryMax := spec.MemoryMaxMB
	if memoryMax <= 0 {
		memoryMax = memory
	}

	return &api.Resources{
		CPU:         intPtr(cpu),
		MemoryMB:    intPtr(memory),
		MemoryMaxMB: intPtr(memoryMax),
		Devices:     []*api.RequestedDevice{gpuDeviceRequest(spec.GPUModel, spec.MinVRAMBytes)},
	}
}

// gpuDeviceRequest declares a device constraint requesting one NVIDIA GPU,
// optionally pinned to a specific model and a minimum VRAM size, so the
// Nomad scheduler only places this job on hardware that can actually run it.
func gpuDeviceRequest(model string, minVRAMBytes int64) *api.RequestedDevice {
	name := "nvidia/gpu"
	if model != "" {
		name = fmt.Sprintf("nvidia/gpu/%s", model)
	}

	req := &api.RequestedDevice{
		Name:  name,
		Count: uint64Ptr(1),
	}

	if minVRAMBytes > 0 {
		req.Constraints = []*api.Constraint{
			api.NewConstraint("${device.attr.memory}", ">=", formatGiB(minVRAMBytes)),
		}
	}

	return req
}

// formatGiB renders bytes as a whole-number GiB string in the form
// Nomad's device attribute constraints expect (e.g. "16 GiB"), rounding
// up so the constraint never under-requests memory.
func formatGiB(bytes int64) string {
	const gib = 1024 * 1024 * 1024
	gibCount := (bytes + gib - 1) / gib
	return fmt.Sprintf("%d GiB", gibCount)
}

func orDefault(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

// nvproxyEnv are the environment variables gVisor's nvproxy reads (in
// place of the nvidia-container-runtime hook, which --runtime=runsc
// bypasses) to decide which GPUs to expose inside the sandbox and which
// driver capability classes the sandboxed process may use.
func nvproxyEnv() map[string]string {
	return map[string]string{
		"NVIDIA_VISIBLE_DEVICES":     "all",
		"NVIDIA_DRIVER_CAPABILITIES": "compute,utility",
	}
}

// mergeEnv layers override on top of base, giving the caller-supplied
// spec.Env the final say over any key the sandbox baseline also sets.
func mergeEnv(base, override map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range override {
		merged[k] = v
	}
	return merged
}

func intPtr(v int) *int                          { return &v }
func stringPtr(v string) *string                 { return &v }
func durationPtr(v time.Duration) *time.Duration { return &v }
func uint64Ptr(v uint64) *uint64                 { return &v }
