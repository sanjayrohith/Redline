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
}

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

	task := api.NewTask("vllm", "docker")
	task.Config = map[string]any{
		"image": spec.Image,
	}
	task.Env = spec.Env
	task.Resources = &api.Resources{
		Devices: []*api.RequestedDevice{gpuDeviceRequest(spec.GPUModel, spec.MinVRAMBytes)},
	}

	tg.AddTask(task)
	job.AddTaskGroup(tg)

	return job
}

// InferenceJobID derives the deterministic Nomad job ID for a deployment,
// so submission, lookup, and stop all agree on the same identifier.
func InferenceJobID(deploymentID string) string {
	return fmt.Sprintf("inference-%s", deploymentID)
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

func intPtr(v int) *int                          { return &v }
func stringPtr(v string) *string                 { return &v }
func durationPtr(v time.Duration) *time.Duration { return &v }
func uint64Ptr(v uint64) *uint64                 { return &v }
