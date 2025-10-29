package metrics

import "github.com/prometheus/client_golang/prometheus"

// GPUCollectors exposes per-device GPU state - utilization, VRAM,
// temperature, power draw - labeled by the node it was sampled on and
// the allocation currently bound to that device (see
// scheduler.GPUBinding), so a dashboard can pivot from a hot or
// VRAM-pressured device straight to the tenant workload responsible.
type GPUCollectors struct {
	utilization *prometheus.GaugeVec
	vramUsed    *prometheus.GaugeVec
	vramTotal   *prometheus.GaugeVec
	temperature *prometheus.GaugeVec
	powerDraw   *prometheus.GaugeVec
}

// NewGPUCollectors builds the per-device GPU gauges and registers them
// against reg.
func NewGPUCollectors(reg *Registry) *GPUCollectors {
	labels := []string{"node", "device", "allocation"}

	utilization := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redline_gpu_utilization_percent", Help: "GPU compute utilization, as reported by the driver.",
	}, labels)
	vramUsed := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redline_gpu_vram_used_bytes", Help: "GPU memory currently in use.",
	}, labels)
	vramTotal := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redline_gpu_vram_total_bytes", Help: "GPU memory total capacity.",
	}, labels)
	temperature := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redline_gpu_temperature_celsius", Help: "GPU core temperature.",
	}, labels)
	powerDraw := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redline_gpu_power_draw_watts", Help: "GPU instantaneous power draw.",
	}, labels)

	reg.MustRegister(utilization, vramUsed, vramTotal, temperature, powerDraw)

	return &GPUCollectors{
		utilization: utilization,
		vramUsed:    vramUsed,
		vramTotal:   vramTotal,
		temperature: temperature,
		powerDraw:   powerDraw,
	}
}

// Update sets every gauge from samples, labeled by nodeID and each
// sample's device index. allocationsByDevice maps a device index to the
// allocation currently bound to it (see scheduler.GPUBinding.TenantFor);
// a device absent from the map - idle, between allocations - is labeled
// with an empty allocation rather than skipped, so its utilization and
// VRAM readings (which are never simply zero: a "freed" device still
// carries whatever the previous tenant left until VRAM zeroing runs) stay
// observable.
func (c *GPUCollectors) Update(nodeID string, allocationsByDevice map[string]string, samples []GPUSample) {
	for _, s := range samples {
		allocation := allocationsByDevice[s.DeviceIndex]
		c.utilization.WithLabelValues(nodeID, s.DeviceIndex, allocation).Set(s.UtilizationPercent)
		c.vramUsed.WithLabelValues(nodeID, s.DeviceIndex, allocation).Set(float64(s.VRAMUsedBytes))
		c.vramTotal.WithLabelValues(nodeID, s.DeviceIndex, allocation).Set(float64(s.VRAMTotalBytes))
		c.temperature.WithLabelValues(nodeID, s.DeviceIndex, allocation).Set(s.TemperatureCelsius)
		c.powerDraw.WithLabelValues(nodeID, s.DeviceIndex, allocation).Set(s.PowerDrawWatts)
	}
}
