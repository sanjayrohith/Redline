package metrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/sanjayrohith/redline/internal/metrics"
)

func TestSampleGPUs_AgainstRealHardware(t *testing.T) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		t.Skip("skipping: nvidia-smi not found on PATH")
	}

	samples, err := metrics.SampleGPUs(context.Background())
	if err != nil {
		t.Fatalf("SampleGPUs() error = %v", err)
	}
	if len(samples) == 0 {
		t.Fatal("SampleGPUs() returned no samples on a node with nvidia-smi present")
	}

	for _, s := range samples {
		if s.DeviceIndex == "" {
			t.Error("sample has an empty DeviceIndex")
		}
		if s.VRAMTotalBytes <= 0 {
			t.Errorf("VRAMTotalBytes = %d, want > 0", s.VRAMTotalBytes)
		}
		if s.VRAMUsedBytes < 0 || s.VRAMUsedBytes > s.VRAMTotalBytes {
			t.Errorf("VRAMUsedBytes = %d, want in [0, %d]", s.VRAMUsedBytes, s.VRAMTotalBytes)
		}
		if s.UtilizationPercent < 0 || s.UtilizationPercent > 100 {
			t.Errorf("UtilizationPercent = %v, want in [0, 100]", s.UtilizationPercent)
		}
	}
}

func TestGPUCollectors_UpdateLabelsByNodeDeviceAndAllocation(t *testing.T) {
	reg := metrics.NewRegistry()
	collectors := metrics.NewGPUCollectors(reg)

	samples := []metrics.GPUSample{
		{DeviceIndex: "0", UtilizationPercent: 87, VRAMUsedBytes: 20 << 30, VRAMTotalBytes: 24 << 30, TemperatureCelsius: 68, PowerDrawWatts: 250},
		{DeviceIndex: "1", UtilizationPercent: 0, VRAMUsedBytes: 0, VRAMTotalBytes: 24 << 30, TemperatureCelsius: 40, PowerDrawWatts: 30},
	}
	collectors.Update("node-a", map[string]string{"0": "alloc-123"}, samples)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()

	if !strings.Contains(body, `redline_gpu_utilization_percent{allocation="alloc-123",device="0",node="node-a"} 87`) {
		t.Errorf("body missing device 0's labeled utilization sample; body:\n%s", body)
	}
	if !strings.Contains(body, `redline_gpu_utilization_percent{allocation="",device="1",node="node-a"} 0`) {
		t.Errorf("body missing device 1's idle (empty allocation) utilization sample; body:\n%s", body)
	}
	if !strings.Contains(body, `redline_gpu_vram_used_bytes{allocation="alloc-123",device="0",node="node-a"}`) {
		t.Errorf("body missing device 0's vram_used sample; body:\n%s", body)
	}
}
