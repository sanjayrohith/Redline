package telemetry

import (
	"context"
	"time"
)

// GPUSample is the subset of metrics.GPUSample the publisher needs. It is
// declared here, narrow, so this package does not import metrics (which
// itself would pull in the Prometheus client stack this package's
// consumers - a browser-facing WebSocket handler - have no reason to
// depend on).
type GPUSample struct {
	DeviceIndex        string
	UtilizationPercent float64
	VRAMUsedBytes      int64
	VRAMTotalBytes     int64
}

// GPUSampleFunc polls every GPU on the local node. It is satisfied by
// metrics.SampleGPUs.
type GPUSampleFunc func(ctx context.Context) ([]GPUSample, error)

// PublishGPUSamples polls sample every interval and publishes one Sample
// per device to hub, until ctx is done. A polling error is swallowed
// rather than stopping the loop - a node with no GPU, or a transient
// nvidia-smi failure, should not take down live telemetry for every other
// signal the hub carries.
func PublishGPUSamples(ctx context.Context, hub *Hub, sample GPUSampleFunc, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			readings, err := sample(ctx)
			if err != nil {
				continue
			}
			now := time.Now()
			for _, r := range readings {
				used, total, util := r.VRAMUsedBytes, r.VRAMTotalBytes, r.UtilizationPercent
				hub.Publish(Sample{
					RunID:     "gpu:" + r.DeviceIndex,
					VRAMBytes: &used, VRAMTotalBytes: &total, GPUUtilizationPct: &util,
					SampledAt: now,
				})
			}
		}
	}
}
