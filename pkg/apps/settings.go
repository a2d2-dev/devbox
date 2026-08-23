package apps

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var composeResourceValue = regexp.MustCompile("^[0-9]+(?:\\.[0-9]+)?(?:[kKmMgG][bB]?)?$")

// applyDeploymentSettings 把向导设置合并到第一个 service。复杂多 service 场景
// 仍由用户直接编辑 Compose；此函数只提供明确、可审计的便捷配置。
func applyDeploymentSettings(raw string, settings *DeploymentSettings) (string, error) {
	if settings == nil {
		return raw, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return "", ValidationErr("compose 解析失败: " + err.Error())
	}
	root := documentMapping(&doc)
	services := mappingValue(root, "services")
	if services == nil || services.Kind != yaml.MappingNode || len(services.Content) < 2 {
		return "", ValidationErr("compose has no services")
	}
	service := services.Content[1]
	if service.Kind != yaml.MappingNode {
		return "", ValidationErr("first compose service must be a mapping")
	}

	restart := "no"
	if settings.AutoStart {
		restart = "unless-stopped"
	}
	setMappingScalar(service, "restart", restart)

	dataPath := strings.TrimSpace(settings.DataPath)
	dataTarget := strings.TrimSpace(settings.DataTarget)
	if dataPath != "" || dataTarget != "" {
		if !filepath.IsAbs(dataPath) || !strings.HasPrefix(dataTarget, "/") {
			return "", ValidationErr("数据路径和容器挂载点都必须是绝对路径")
		}
		volumes := ensureSequence(service, "volumes")
		volumes.Content = append(volumes.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: filepath.Clean(dataPath) + ":" + filepath.Clean(dataTarget)})
	}

	cpu := strings.TrimSpace(settings.CPULimit)
	memory := strings.TrimSpace(settings.MemoryLimit)
	if cpu != "" || memory != "" {
		if cpu != "" && !composeResourceValue.MatchString(cpu) {
			return "", ValidationErr("CPU 限制格式无效")
		}
		if memory != "" && !composeResourceValue.MatchString(memory) {
			return "", ValidationErr("内存限制格式无效")
		}
		deploy := ensureMapping(service, "deploy")
		resources := ensureMapping(deploy, "resources")
		limits := ensureMapping(resources, "limits")
		if cpu != "" {
			setMappingScalar(limits, "cpus", cpu)
		}
		if memory != "" {
			setMappingScalar(limits, "memory", memory)
		}
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", fmt.Errorf("render compose settings: %w", err)
	}
	return string(out), nil
}

func documentMapping(doc *yaml.Node) *yaml.Node {
	if doc != nil && doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return doc
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func ensureMapping(parent *yaml.Node, key string) *yaml.Node {
	if found := mappingValue(parent, key); found != nil && found.Kind == yaml.MappingNode {
		return found
	}
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setMappingNode(parent, key, node)
	return node
}

func ensureSequence(parent *yaml.Node, key string) *yaml.Node {
	if found := mappingValue(parent, key); found != nil && found.Kind == yaml.SequenceNode {
		return found
	}
	node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	setMappingNode(parent, key, node)
	return node
}

func setMappingScalar(parent *yaml.Node, key, value string) {
	setMappingNode(parent, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func setMappingNode(parent *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content[i+1] = value
			return
		}
	}
	parent.Content = append(parent.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}
