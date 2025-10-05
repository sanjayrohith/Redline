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

	tg.AddTask(task)
	job.AddTaskGroup(tg)

	return job
}

// InferenceJobID derives the deterministic Nomad job ID for a deployment,
// so submission, lookup, and stop all agree on the same identifier.
func InferenceJobID(deploymentID string) string {
	return fmt.Sprintf("inference-%s", deploymentID)
}

func intPtr(v int) *int                          { return &v }
func stringPtr(v string) *string                 { return &v }
func durationPtr(v time.Duration) *time.Duration { return &v }
