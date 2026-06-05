package collector

import (
	"os/exec"
	"strconv"
	"strings"
)

// collectGPU 采集 GPU 信息（通过 nvidia-smi）
// 如果 nvidia-smi 不可用，返回空切片
func collectGPU() []GPUInfo {
	path, err := exec.LookPath("nvidia-smi")
	if err != nil || path == "" {
		return nil
	}

	// nvidia-smi CSV 模式，参考 1Panel 的采集方式
	cmd := exec.Command("nvidia-smi",
		"--query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,power.limit,fan.speed",
		"--format=csv,noheader,nounits",
	)

	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var gpus []GPUInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, ", ")
		if len(fields) < 9 {
			continue
		}

		gpu := GPUInfo{}
		gpu.Index, _ = strconv.Atoi(strings.TrimSpace(fields[0]))
		gpu.ProductName = strings.TrimSpace(fields[1])
		gpu.GPUUtil, _ = strconv.ParseFloat(strings.TrimSpace(fields[2]), 64)
		memUsed, _ := strconv.ParseUint(strings.TrimSpace(fields[3]), 10, 64)
		gpu.MemUsed = memUsed
		memTotal, _ := strconv.ParseUint(strings.TrimSpace(fields[4]), 10, 64)
		gpu.MemTotal = memTotal
		gpu.Temperature, _ = strconv.Atoi(strings.TrimSpace(fields[5]))
		gpu.PowerDraw, _ = strconv.ParseFloat(strings.TrimSpace(fields[6]), 64)
		gpu.PowerLimit, _ = strconv.ParseFloat(strings.TrimSpace(fields[7]), 64)
		gpu.FanSpeed, _ = strconv.Atoi(strings.TrimSpace(fields[8]))

		gpus = append(gpus, gpu)
	}

	return gpus
}
