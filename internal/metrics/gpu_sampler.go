package metrics

import (
	"context"
	"encoding/csv"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// GPUSample is one point-in-time reading from a single physical GPU
// device, as reported by the driver.
type GPUSample struct {
	DeviceIndex        string
	UtilizationPercent float64
	VRAMUsedBytes      int64
	VRAMTotalBytes     int64
	TemperatureCelsius float64
	PowerDrawWatts     float64
}

// gpuQueryFields is the nvidia-smi --query-gpu field list SampleGPUs
// requests, in the exact order its CSV output returns them.
const gpuQueryFields = "index,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw"

// SampleGPUs polls every GPU on the local node for utilization, VRAM
// usage, temperature, and power draw.
//
// This shells out to nvidia-smi rather than linking NVML directly (e.g.
// via cgo bindings): nvidia-smi's own utilization/memory/temperature/
// power queries are themselves backed by NVML - it is NVIDIA's own NVML
// client - so the data sourced here is the same driver-level data an
// NVML binding would read, without requiring CGO_ENABLED=1 or
// libnvidia-ml.so present at build time for every environment that
// builds this binary, only at runtime on nodes that actually have a GPU.
func SampleGPUs(ctx context.Context) ([]GPUSample, error) {
	out, err := exec.CommandContext(ctx, "nvidia-smi", // #nosec G204 -- fixed arguments, no user input
		"--query-gpu="+gpuQueryFields, "--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil, fmt.Errorf("metrics: sample gpus: %w", err)
	}

	reader := csv.NewReader(strings.NewReader(string(out)))
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("metrics: parse nvidia-smi output: %w", err)
	}

	samples := make([]GPUSample, 0, len(records))
	for _, rec := range records {
		if len(rec) != 6 {
			return nil, fmt.Errorf("metrics: nvidia-smi row has %d fields, want 6: %v", len(rec), rec)
		}

		util, err := strconv.ParseFloat(strings.TrimSpace(rec[1]), 64)
		if err != nil {
			return nil, fmt.Errorf("metrics: parse utilization %q: %w", rec[1], err)
		}
		usedMiB, err := strconv.ParseInt(strings.TrimSpace(rec[2]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("metrics: parse memory.used %q: %w", rec[2], err)
		}
		totalMiB, err := strconv.ParseInt(strings.TrimSpace(rec[3]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("metrics: parse memory.total %q: %w", rec[3], err)
		}
		tempC, err := strconv.ParseFloat(strings.TrimSpace(rec[4]), 64)
		if err != nil {
			return nil, fmt.Errorf("metrics: parse temperature.gpu %q: %w", rec[4], err)
		}
		powerW, err := strconv.ParseFloat(strings.TrimSpace(rec[5]), 64)
		if err != nil {
			return nil, fmt.Errorf("metrics: parse power.draw %q: %w", rec[5], err)
		}

		const bytesPerMiB = 1024 * 1024
		samples = append(samples, GPUSample{
			DeviceIndex:        strings.TrimSpace(rec[0]),
			UtilizationPercent: util,
			VRAMUsedBytes:      usedMiB * bytesPerMiB,
			VRAMTotalBytes:     totalMiB * bytesPerMiB,
			TemperatureCelsius: tempC,
			PowerDrawWatts:     powerW,
		})
	}

	return samples, nil
}
