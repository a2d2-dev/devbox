package apps

import (
	"context"
	"errors"
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

// kubernetesRuntime：现有 K8s 应用管理迁移为 runtimeAdapter（阶段0）。
// deploymentToAppInfo 收敛为 deploymentToApplication，Phase 聚合在后端完成。
type kubernetesRuntime struct {
	client    kubernetes.Interface
	namespace string
	logger    *zap.Logger
}

// KubeConfig K8s 连接配置。
type KubeConfig struct {
	Kubeconfig string
	Namespace  string
}

// NewKubernetesRuntime 创建 K8s adapter。client 不可用时返回 error（main 层降级）。
func NewKubernetesRuntime(logger *zap.Logger, cfg KubeConfig) (*kubernetesRuntime, error) {
	var config *rest.Config
	var err error
	if cfg.Kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", cfg.Kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("build k8s config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create k8s client: %w", err)
	}
	ns := cfg.Namespace
	if ns == "" {
		ns = "default"
	}
	return &kubernetesRuntime{client: clientset, namespace: ns, logger: logger}, nil
}

func (k *kubernetesRuntime) Kind() RuntimeKind { return RuntimeKubernetes }

// Capability 探测 K8s 可用性（list deployments 探活）。
func (k *kubernetesRuntime) Capability(ctx context.Context) RuntimeCapability {
	ver, err := k.client.Discovery().ServerVersion()
	if err != nil {
		return RuntimeCapability{Available: false, Reason: "k8s api 不可达: " + err.Error()}
	}
	return RuntimeCapability{
		Available: true,
		Version:   ver.GitVersion,
		Features:  []string{"start", "stop", "restart", "remove", "logs"},
	}
}

// Observe 列出所有 Deployment 为 Application。
func (k *kubernetesRuntime) Observe(ctx context.Context) (map[string]Application, error) {
	deployments, err := k.client.AppsV1().Deployments(k.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		// 不可达不报错刷屏；controller 层降级。
		return map[string]Application{}, nil
	}
	out := make(map[string]Application, len(deployments.Items))
	for i := range deployments.Items {
		app := deploymentToApplication(&deployments.Items[i])
		out[app.ID] = app
	}
	return out, nil
}

// Apply K8s 不通过 Controller.Apply 创建（商店流程在阶段4 统一接入）。
func (k *kubernetesRuntime) Apply(ctx context.Context, app Application, composeFile string) error {
	return errors.New("kubernetes apply via controller is not supported; use the app store")
}

// Operate start/stop/restart。
func (k *kubernetesRuntime) Operate(ctx context.Context, app Application, action Action) error {
	switch action {
	case ActionStart:
		return k.scale(ctx, app.ID, 1)
	case ActionStop:
		return k.scale(ctx, app.ID, 0)
	case ActionRestart, ActionRedeploy:
		return k.restart(ctx, app.ID)
	default:
		return ValidationErr("unknown action: " + string(action))
	}
}

// Remove 删除 Deployment + 关联 Service。
func (k *kubernetesRuntime) Remove(ctx context.Context, app Application, purge bool) error {
	if err := k.client.AppsV1().Deployments(k.namespace).Delete(ctx, app.ID, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete deployment %s: %w", app.ID, err)
	}
	_ = k.client.CoreV1().Services(k.namespace).Delete(ctx, app.ID, metav1.DeleteOptions{})
	return nil
}

// Logs 取 Deployment 第一个 Pod 的日志。
func (k *kubernetesRuntime) Logs(ctx context.Context, app Application, opts LogOptions) (LogPage, error) {
	pods, err := k.client.CoreV1().Pods(k.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", app.ID),
	})
	if err != nil {
		return LogPage{}, fmt.Errorf("list pods for %s: %w", app.ID, err)
	}
	if len(pods.Items) == 0 {
		return LogPage{AppID: app.ID, Logs: ""}, nil
	}
	pod := pods.Items[0]
	podOpts := &corev1.PodLogOptions{}
	if opts.Tail > 0 {
		t := opts.Tail
		podOpts.TailLines = &t
	}
	req := k.client.CoreV1().Pods(k.namespace).GetLogs(pod.Name, podOpts)
	raw, err := req.DoRaw(ctx)
	if err != nil {
		return LogPage{}, fmt.Errorf("get logs for pod %s: %w", pod.Name, err)
	}
	return LogPage{AppID: app.ID, Logs: sanitizeLog(string(raw))}, nil
}

func (k *kubernetesRuntime) scale(ctx context.Context, name string, replicas int32) error {
	scale, err := k.client.AppsV1().Deployments(k.namespace).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get scale %s: %w", name, err)
	}
	scale.Spec.Replicas = replicas
	if _, err := k.client.AppsV1().Deployments(k.namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("scale %s to %d: %w", name, replicas, err)
	}
	return nil
}

func (k *kubernetesRuntime) restart(ctx context.Context, name string) error {
	d, err := k.client.AppsV1().Deployments(k.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get deployment %s: %w", name, err)
	}
	if d.Spec.Template.Annotations == nil {
		d.Spec.Template.Annotations = map[string]string{}
	}
	d.Spec.Template.Annotations["devbox.a2d2.dev/restartedAt"] = time.Now().UTC().Format(time.RFC3339)
	if _, err := k.client.AppsV1().Deployments(k.namespace).Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("restart deployment %s: %w", name, err)
	}
	return nil
}

// deploymentToApplication 把 K8s Deployment 收敛为对外 Application。
// 兼容字段（State/Image/Ports/Replicas/Ready/CPU/Mem）保持旧 deploymentToAppInfo 语义。
func deploymentToApplication(d *appsv1.Deployment) Application {
	app := Application{
		ID:        d.Name,
		Name:      d.Name,
		Kind:      "app",
		Runtime:   RuntimeKubernetes,
		Namespace: d.Namespace,
		CreatedAt: d.CreationTimestamp.Time,
		Revision:  0,
	}
	app.Observed.Revision = 0

	replicas := int32(1)
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}
	ready := d.Status.ReadyReplicas
	app.Replicas = replicas
	app.Ready = ready

	// Phase 聚合：按 Deployment conditions 区分 failed/deploying，避免 rollout
	// 进行中（ready==0）被误判为 failed。
	app.Observed.Phase = deploymentPhase(d, replicas, ready)
	app.State = app.Observed.Phase.LegacyState()

	svc := ServiceStatus{Name: d.Name, Replicas: replicas, Ready: ready, State: stateFromPhase(app.Observed.Phase)}

	if len(d.Spec.Template.Spec.Containers) > 0 {
		c := d.Spec.Template.Spec.Containers[0]
		app.Image = c.Image
		svc.Image = c.Image
		if parts := strings.SplitN(c.Image, ":", 2); len(parts) == 2 {
			app.Version = parts[1]
		} else {
			app.Version = "latest"
		}
		if req := c.Resources.Requests; req != nil {
			if cpu, ok := req[corev1.ResourceCPU]; ok {
				app.CPURequest = cpu.String()
			}
			if mem, ok := req[corev1.ResourceMemory]; ok {
				app.MemRequest = mem.String()
			}
		}
		if lim := c.Resources.Limits; lim != nil {
			if cpu, ok := lim[corev1.ResourceCPU]; ok {
				app.CPULimit = cpu.String()
			}
			if mem, ok := lim[corev1.ResourceMemory]; ok {
				app.MemLimit = mem.String()
			}
		}
		for _, p := range c.Ports {
			pm := PortMapping{Name: p.Name, ContainerPort: p.ContainerPort, Protocol: string(p.Protocol)}
			if p.HostPort > 0 {
				pm.HostPort = p.HostPort
			}
			app.Ports = append(app.Ports, pm)
			svc.Ports = append(svc.Ports, pm)
		}
	}
	app.Observed.Services = []ServiceStatus{svc}
	return app
}

func stateFromPhase(p Phase) string {
	switch p {
	case PhaseRunning:
		return "running"
	case PhaseStopped:
		return "exited"
	case PhaseFailed:
		return "error"
	default:
		return "pending"
	}
}

// findDepCondition 按类型查找 Deployment condition（不存在返回 nil）。
func findDepCondition(d *appsv1.Deployment, t appsv1.DeploymentConditionType) *appsv1.DeploymentCondition {
	for i := range d.Status.Conditions {
		if d.Status.Conditions[i].Type == t {
			return &d.Status.Conditions[i]
		}
	}
	return nil
}

// deploymentPhase 基于 replicas/ready + Deployment conditions 聚合 phase。
//
// ready==0 不再直接判 failed：滚动发布/更新初期 ready 为 0 属正常（deploying）。
// 仅当出现 ReplicaFailure 或 Progressing 超过 progressDeadline（False 且 reason
// ProgressDeadlineExceeded）才判 failed。
func deploymentPhase(d *appsv1.Deployment, replicas, ready int32) Phase {
	switch {
	case replicas == 0:
		return PhaseStopped
	case ready >= replicas && replicas > 0:
		return PhaseRunning
	}
	if c := findDepCondition(d, appsv1.DeploymentReplicaFailure); c != nil && c.Status == corev1.ConditionTrue {
		return PhaseFailed
	}
	if prog := findDepCondition(d, appsv1.DeploymentProgressing); prog != nil {
		// Progressing=False 且超时 → 真失败；True/Unknown 都视为进行中。
		if prog.Status == corev1.ConditionFalse && prog.Reason == "ProgressDeadlineExceeded" {
			return PhaseFailed
		}
	}
	return PhaseDeploying
}
