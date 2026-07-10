package apps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Config K8s 连接配置
type Config struct {
	Kubeconfig string `mapstructure:"kubeconfig"`
	Namespace  string `mapstructure:"namespace"`
}

// Manager K8s 应用管理器
type Manager struct {
	client    kubernetes.Interface
	namespace string
	logger    *zap.Logger
}

// NewManager 创建应用管理器
func NewManager(logger *zap.Logger, cfg Config) (*Manager, error) {
	var config *rest.Config
	var err error

	if cfg.Kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", cfg.Kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to build k8s config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	ns := cfg.Namespace
	if ns == "" {
		ns = "default"
	}

	logger.Info("K8s app manager initialized",
		zap.String("namespace", ns))

	return &Manager{
		client:    clientset,
		namespace: ns,
		logger:    logger,
	}, nil
}

// ListApps 列出所有应用（Deployment）
func (m *Manager) ListApps(ctx context.Context) ([]AppInfo, error) {
	deployments, err := m.client.AppsV1().Deployments(m.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	apps := make([]AppInfo, 0, len(deployments.Items))
	for _, d := range deployments.Items {
		apps = append(apps, deploymentToAppInfo(&d))
	}
	return apps, nil
}

// GetApp 获取单个应用
func (m *Manager) GetApp(ctx context.Context, name string) (*AppInfo, error) {
	d, err := m.client.AppsV1().Deployments(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment %s: %w", name, err)
	}
	info := deploymentToAppInfo(d)
	return &info, nil
}

// StartApp 启动应用（scale to 1）
func (m *Manager) StartApp(ctx context.Context, name string) error {
	return m.scaleDeployment(ctx, name, 1)
}

// StopApp 停止应用（scale to 0）
func (m *Manager) StopApp(ctx context.Context, name string) error {
	return m.scaleDeployment(ctx, name, 0)
}

// RestartApp 重启应用（rollout restart）
func (m *Manager) RestartApp(ctx context.Context, name string) error {
	d, err := m.client.AppsV1().Deployments(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment %s: %w", name, err)
	}

	if d.Spec.Template.Annotations == nil {
		d.Spec.Template.Annotations = make(map[string]string)
	}
	d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	_, err = m.client.AppsV1().Deployments(m.namespace).Update(ctx, d, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to restart deployment %s: %w", name, err)
	}

	m.logger.Info("App restarted", zap.String("name", name))
	return nil
}

// DeleteApp 卸载应用（删除 Deployment）
func (m *Manager) DeleteApp(ctx context.Context, name string) error {
	err := m.client.AppsV1().Deployments(m.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete deployment %s: %w", name, err)
	}

	// 也尝试删除关联的 Service
	_ = m.client.CoreV1().Services(m.namespace).Delete(ctx, name, metav1.DeleteOptions{})

	m.logger.Info("App deleted", zap.String("name", name))
	return nil
}

// GetLogs 获取应用日志
func (m *Manager) GetLogs(ctx context.Context, name string, tailLines int64) (string, error) {
	// 找到 Deployment 对应的 Pod
	pods, err := m.client.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", name),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods for %s: %w", name, err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods found for deployment %s", name)
	}

	// 取第一个 Pod 的日志
	pod := pods.Items[0]
	opts := &corev1.PodLogOptions{
		TailLines: &tailLines,
	}

	req := m.client.CoreV1().Pods(m.namespace).GetLogs(pod.Name, opts)
	result, err := req.DoRaw(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get logs for pod %s: %w", pod.Name, err)
	}

	return string(result), nil
}

func (m *Manager) scaleDeployment(ctx context.Context, name string, replicas int32) error {
	scale, err := m.client.AppsV1().Deployments(m.namespace).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get scale for %s: %w", name, err)
	}

	scale.Spec.Replicas = replicas
	_, err = m.client.AppsV1().Deployments(m.namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to scale %s to %d: %w", name, replicas, err)
	}

	m.logger.Info("App scaled", zap.String("name", name), zap.Int32("replicas", replicas))
	return nil
}

func deploymentToAppInfo(d *appsv1.Deployment) AppInfo {
	info := AppInfo{
		ID:        d.Name,
		Name:      d.Name,
		Namespace: d.Namespace,
		Kind:      "app",
		Replicas:  1,
		CreatedAt: d.CreationTimestamp.Time,
	}

	if d.Spec.Replicas != nil {
		info.Replicas = *d.Spec.Replicas
	}
	info.Ready = d.Status.ReadyReplicas

	// State
	switch {
	case info.Replicas == 0:
		info.State = "stopped"
	case info.Ready == info.Replicas:
		info.State = "running"
	case info.Ready == 0:
		info.State = "error"
	default:
		info.State = "pending"
	}

	// Image + version
	if len(d.Spec.Template.Spec.Containers) > 0 {
		c := d.Spec.Template.Spec.Containers[0]
		info.Image = c.Image
		// 从 image tag 提取 version
		if parts := strings.SplitN(c.Image, ":", 2); len(parts) == 2 {
			info.Version = parts[1]
		}

		// Resources
		if req := c.Resources.Requests; req != nil {
			if cpu, ok := req[corev1.ResourceCPU]; ok {
				info.CPURequest = cpu.String()
			}
			if mem, ok := req[corev1.ResourceMemory]; ok {
				info.MemRequest = mem.String()
			}
		}
		if lim := c.Resources.Limits; lim != nil {
			if cpu, ok := lim[corev1.ResourceCPU]; ok {
				info.CPULimit = cpu.String()
			}
			if mem, ok := lim[corev1.ResourceMemory]; ok {
				info.MemLimit = mem.String()
			}
		}

		// Ports
		for _, p := range c.Ports {
			info.Ports = append(info.Ports, PortMapping{
				Name:          p.Name,
				ContainerPort: p.ContainerPort,
				Protocol:      string(p.Protocol),
			})
		}
	}

	return info
}
