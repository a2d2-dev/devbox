package collector

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestMetricsHistoryWindowAndDownsampling(t *testing.T) {
	c := New(zap.NewNop(), "test")
	base := time.Now().Add(-2 * time.Hour)
	for i := 0; i < 120; i++ {
		c.history[i] = SystemMetrics{ShotTime: base.Add(time.Duration(i) * time.Minute), CPUUsedPercent: float64(i)}
	}
	c.historyIdx = 120
	history := c.GetMetricsHistoryWindow(time.Hour, 10)
	if len(history.Timestamps) > 10 {
		t.Fatalf("expected at most 10 points, got %d", len(history.Timestamps))
	}
	if len(history.Timestamps) == 0 || history.Timestamps[len(history.Timestamps)-1] != base.Add(119*time.Minute) {
		t.Fatalf("latest point missing: %#v", history.Timestamps)
	}
	if history.CPUPercent[0] < 59 {
		t.Fatalf("window was not applied: first cpu point=%v", history.CPUPercent[0])
	}
}

func TestMetricsHistoryDownsamplingHonorsExactLimit(t *testing.T) {
	c := New(zap.NewNop(), "test")
	base := time.Now().Add(-24 * time.Hour)
	for i := 0; i < 1440; i++ {
		c.history[i] = SystemMetrics{ShotTime: base.Add(time.Duration(i) * time.Minute), CPUUsedPercent: float64(i)}
	}
	c.historyIdx = 1440

	history := c.GetMetricsHistoryWindow(24*time.Hour, 720)
	if len(history.Timestamps) != 720 {
		t.Fatalf("expected exactly 720 points, got %d", len(history.Timestamps))
	}
	if history.Timestamps[0] != base || history.Timestamps[len(history.Timestamps)-1] != base.Add(1439*time.Minute) {
		t.Fatalf("history endpoints missing: first=%v last=%v", history.Timestamps[0], history.Timestamps[len(history.Timestamps)-1])
	}
}

func TestReadBlockDeviceTemperatureAt(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nvme0n1", "device", "hwmon", "hwmon7")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "temp1_input"), []byte("42125\n"), 0644); err != nil {
		t.Fatal(err)
	}
	temp, ok := readBlockDeviceTemperatureAt(root, "nvme0n1")
	if !ok || temp != 42.125 {
		t.Fatalf("temp=%v ok=%v", temp, ok)
	}
	if _, ok := readBlockDeviceTemperatureAt(root, "sda"); ok {
		t.Fatal("missing sensor must be reported as unsupported")
	}
}
