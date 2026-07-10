package models

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Config 模型仓库配置
type Config struct {
	ScanDirs       []string `mapstructure:"scan_dirs"`
	OllamaEndpoint string   `mapstructure:"ollama_endpoint"`
}

// ModelInfo 模型信息
type ModelInfo struct {
	Name     string    `json:"name"`
	Family   string    `json:"family"`
	Size     string    `json:"size"`
	Format   string    `json:"format"`
	Engine   string    `json:"engine"`
	Tags     []string  `json:"tags,omitempty"`
	Source   string    `json:"source"` // "local" | "ollama"
	Modified time.Time `json:"modified,omitempty"`
}

// Scanner 模型扫描器
type Scanner struct {
	scanDirs       []string
	ollamaEndpoint string
}

// NewScanner 创建扫描器
func NewScanner(cfg Config) *Scanner {
	dirs := cfg.ScanDirs
	if len(dirs) == 0 {
		dirs = []string{"/workspace/models"}
	}
	endpoint := cfg.OllamaEndpoint
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	return &Scanner{scanDirs: dirs, ollamaEndpoint: endpoint}
}

// Scan 扫描本地模型
func (s *Scanner) Scan() []ModelInfo {
	var models []ModelInfo

	// 1. 扫描目录
	for _, dir := range s.scanDirs {
		models = append(models, s.scanDir(dir)...)
	}

	// 2. Ollama 模型
	models = append(models, s.scanOllama()...)

	return models
}

func (s *Scanner) scanDir(dir string) []ModelInfo {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var models []ModelInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}

		model := ModelInfo{
			Name:     e.Name(),
			Source:   "local",
			Modified: info.ModTime(),
		}

		// 推断格式
		subEntries, _ := os.ReadDir(filepath.Join(dir, e.Name()))
		for _, sub := range subEntries {
			name := sub.Name()
			switch {
			case strings.HasSuffix(name, ".safetensors"):
				model.Format = "safetensors"
			case strings.HasSuffix(name, ".gguf"):
				model.Format = "gguf"
			case strings.HasSuffix(name, ".bin"):
				if model.Format == "" {
					model.Format = "bin"
				}
			}
		}

		// 推断 family
		nameLower := strings.ToLower(e.Name())
		switch {
		case strings.Contains(nameLower, "llama"):
			model.Family = "Llama"
		case strings.Contains(nameLower, "qwen"):
			model.Family = "Qwen"
		case strings.Contains(nameLower, "deepseek"):
			model.Family = "DeepSeek"
		case strings.Contains(nameLower, "glm"):
			model.Family = "GLM"
		case strings.Contains(nameLower, "stable-diffusion") || strings.Contains(nameLower, "sdxl"):
			model.Family = "SD"
		case strings.Contains(nameLower, "flux"):
			model.Family = "Flux"
		case strings.Contains(nameLower, "whisper"):
			model.Family = "Whisper"
		case strings.Contains(nameLower, "bge"):
			model.Family = "BGE"
		}

		// 计算目录大小（粗略）
		model.Size = dirSizeHuman(filepath.Join(dir, e.Name()))

		models = append(models, model)
	}

	return models
}

func (s *Scanner) scanOllama() []ModelInfo {
	// 尝试 ollama list
	cmd := exec.Command("ollama", "list")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var models []ModelInfo
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] == "NAME" {
			continue
		}
		model := ModelInfo{
			Name:   fields[0],
			Source: "ollama",
			Engine: "Ollama",
			Format: "gguf",
		}
		if len(fields) >= 3 {
			model.Size = fields[2]
		}
		models = append(models, model)
	}

	return models
}

// OllamaModel ollama API 返回的模型结构
type OllamaModel struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

// ScanOllamaAPI 通过 Ollama HTTP API 扫描
func (s *Scanner) ScanOllamaAPI() []ModelInfo {
	cmd := exec.Command("curl", "-s", s.ollamaEndpoint+"/api/tags")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var resp struct {
		Models []OllamaModel `json:"models"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil
	}

	var models []ModelInfo
	for _, m := range resp.Models {
		models = append(models, ModelInfo{
			Name:     m.Name,
			Source:   "ollama",
			Engine:   "Ollama",
			Format:   "gguf",
			Size:     formatBytes(m.Size),
			Modified: m.ModifiedAt,
		})
	}
	return models
}

func dirSizeHuman(path string) string {
	var total int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	return formatBytes(total)
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<40:
		return fmt.Sprintf("%.1f TB", float64(b)/float64(1<<40))
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
