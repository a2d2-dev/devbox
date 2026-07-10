// Package gpuhistory 维护 GPU 指标的时间序列环形缓冲，
// 后台每 interval 采样一次 nvidia-smi，前端按 window 查询。
package gpuhistory

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Point struct {
	TimestampMS int64   `json:"t"`     // unix ms
	UtilPct     int     `json:"util"`
	MemUsedMiB  int     `json:"memUsed"`
	MemTotalMiB int     `json:"memTotal"`
	TempC       float64 `json:"temp"`
	PowerDrawW  float64 `json:"power"`
}

type Collector struct {
	interval time.Duration
	capacity int

	mu     sync.RWMutex
	buf    []Point
	head   int // 下一个写入位置
	filled bool
}

func New(interval time.Duration, retainFor time.Duration) *Collector {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	capacity := int(retainFor / interval)
	if capacity < 60 {
		capacity = 60
	}
	return &Collector{
		interval: interval,
		capacity: capacity,
		buf:      make([]Point, capacity),
	}
}

func (c *Collector) Start(ctx context.Context) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return // 没 nvidia-smi 直接不启，前端会拿空曲线
	}
	go func() {
		t := time.NewTicker(c.interval)
		defer t.Stop()
		c.sample() // 立即采一次
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				c.sample()
			}
		}
	}()
}

func (c *Collector) sample() {
	subCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(subCtx, "nvidia-smi",
		"--query-gpu=utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return
	}
	// 只取第一张卡 (多卡以后再展开)
	line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	f := strings.Split(line, ",")
	if len(f) < 5 {
		return
	}
	p := Point{TimestampMS: time.Now().UnixMilli()}
	p.UtilPct, _ = strconv.Atoi(strings.TrimSpace(f[0]))
	p.MemUsedMiB, _ = strconv.Atoi(strings.TrimSpace(f[1]))
	p.MemTotalMiB, _ = strconv.Atoi(strings.TrimSpace(f[2]))
	p.TempC, _ = strconv.ParseFloat(strings.TrimSpace(f[3]), 64)
	p.PowerDrawW, _ = strconv.ParseFloat(strings.TrimSpace(f[4]), 64)

	c.mu.Lock()
	c.buf[c.head] = p
	c.head = (c.head + 1) % c.capacity
	if c.head == 0 {
		c.filled = true
	}
	c.mu.Unlock()
}

// Window 返回最近 dur 时间段的采样点（按时间正序）。dur<=0 返回全部。
func (c *Collector) Window(dur time.Duration) []Point {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []Point
	if c.filled {
		out = append(out, c.buf[c.head:]...)
		out = append(out, c.buf[:c.head]...)
	} else {
		out = append(out, c.buf[:c.head]...)
	}
	if dur <= 0 {
		return out
	}
	cutoff := time.Now().Add(-dur).UnixMilli()
	trimmed := out[:0]
	for _, p := range out {
		if p.TimestampMS >= cutoff {
			trimmed = append(trimmed, p)
		}
	}
	return trimmed
}
