package apps

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// catalog 交互的安全上限（CEO 裁决第10条）。
const (
	// maxCatalogBytes 单次 catalog 响应（列表 / 版本）允许读取的最大字节数。
	// 超过即视为异常并拒绝，避免恶意/失序的 apiserver 用超大响应拖垮 devbox。
	maxCatalogBytes = 4 << 20 // 4 MiB
)

// StoreManager 应用商店管理器（通过 edge-apiserver HTTP API）
type StoreManager struct {
	apiURL     string
	consoleURL string
	logger     *zap.Logger
	client     *http.Client
	token      string // from kubeconfig bearer token if any
}

// StoreConfig 应用商店配置
type StoreConfig struct {
	APIServerURL string `mapstructure:"apiserver_url"`
	ConsoleURL   string `mapstructure:"console_url"`
	Kubeconfig   string `mapstructure:"kubeconfig"`
}

// NewStoreManager 创建应用商店管理器
func NewStoreManager(logger *zap.Logger, cfg StoreConfig) (*StoreManager, error) {
	if cfg.APIServerURL == "" {
		return nil, fmt.Errorf("apiserver_url is required")
	}

	// 从 kubeconfig 获取 TLS 证书用于认证
	var restConfig *rest.Config
	var err error
	if cfg.Kubeconfig != "" {
		restConfig, err = clientcmd.BuildConfigFromFlags("", cfg.Kubeconfig)
	} else {
		restConfig, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to build k8s config: %w", err)
	}

	// 用 kubeconfig 的证书构建 HTTP client
	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	if restConfig.TLSClientConfig.CertData != nil && restConfig.TLSClientConfig.KeyData != nil {
		cert, err := tls.X509KeyPair(restConfig.TLSClientConfig.CertData, restConfig.TLSClientConfig.KeyData)
		if err != nil {
			return nil, fmt.Errorf("failed to load client cert: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	httpClient := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
	}

	return NewStoreManagerWithClient(cfg.APIServerURL, cfg.ConsoleURL, restConfig.BearerToken, httpClient, logger), nil
}

// NewStoreManagerWithClient 用显式注入的 http.Client 构造 StoreManager。
// 生产代码（NewStoreManager）内部调用它；导出是为测试注入 httptest fake catalog，
// 以及未来允许注入自定义 transport（重试/trace）。
func NewStoreManagerWithClient(apiURL, consoleURL, token string, client *http.Client, logger *zap.Logger) *StoreManager {
	return &StoreManager{
		apiURL:     apiURL,
		consoleURL: consoleURL,
		logger:     logger,
		client:     client,
		token:      token,
	}
}

// storeAppsResponse edge-apiserver 返回的 storeapps 列表
type storeAppsResponse struct {
	Items []storeAppItem `json:"items"`
}

type storeAppItem struct {
	Metadata struct {
		Name              string            `json:"name"`
		Labels            map[string]string `json:"labels"`
		Annotations       map[string]string `json:"annotations"`
		CreationTimestamp string            `json:"creationTimestamp"`
	} `json:"metadata"`
	Status struct {
		LatestVersion string `json:"latestVersion"`
		VersionCount  int    `json:"versionCount"`
	} `json:"status"`
}

// provisionerLabel → 本机运行时推断（列表层面粗判）。
// workload/helm/model 均为 Kubernetes 原生包，本机不可 Compose 安装。
// 仅当 catalog 以 provisioner=compose 发布时才乐观视为 Compose 包。
//
// 注意（口径差异）：列表按 provisioner label 粗判，权威值以 GET /store/version
// （版本是否真的带 composeTemplate）为准。若 catalog 标 provisioner=compose 但版本
// 实际无模板，列表会乐观显示「可安装」，但 install 会经 GetStoreAppVersion 重新核对
// 并返回 422——不会发生错误安装，仅一次性 UX 不一致。
func runtimeFromProvisioner(labels map[string]string) (RuntimeKind, []string, bool, string) {
	switch labels["app.theriseunion.io/provisioner"] {
	case "compose":
		return RuntimeCompose, []string{"compose", "kubernetes"}, true, ""
	default:
		// 未知 provisioner（含 workload/helm/model/空）一律按 Kubernetes 处理。
		reason := "仅 Kubernetes 环境支持"
		return RuntimeKubernetes, []string{"kubernetes"}, false, reason
	}
}

// ListStoreApps 从 edge-apiserver 获取应用商店列表
func (s *StoreManager) ListStoreApps(ctx context.Context) ([]StoreApp, error) {
	u := s.apiURL + "/oapis/app.theriseunion.io/v1alpha1/storeapps?limit=1000"

	body, err := s.catalogGet(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("fetch storeapps: %w", err)
	}

	var data storeAppsResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("decode storeapps: %w", err)
	}

	apps := make([]StoreApp, 0, len(data.Items))
	for _, item := range data.Items {
		rt, runtimes, installable, reason := runtimeFromProvisioner(item.Metadata.Labels)
		app := StoreApp{
			ID:                   item.Metadata.Name,
			Name:                 item.Metadata.Annotations["theriseunion.io/alias-name"],
			Category:             item.Metadata.Labels["app.theriseunion.io/category"],
			Description:          item.Metadata.Annotations["theriseunion.io/description"],
			Icon:                 item.Metadata.Annotations["app.theriseunion.io/icon"],
			Provider:             item.Metadata.Annotations["app.theriseunion.io/provider"],
			Version:              item.Status.LatestVersion,
			VersionCount:         item.Status.VersionCount,
			Runtime:              rt,
			Runtimes:             runtimes,
			Installable:          installable,
			NotInstallableReason: reason,
			Pinned:               item.Metadata.Labels["app.theriseunion.io/pinned"] == "true",
			PublishedAt:          orStr(item.Metadata.Annotations["app.theriseunion.io/published-at"], item.Metadata.CreationTimestamp),
			SourceType:           "official",
			TrustLevel:           "reviewed",
		}
		if app.Name == "" {
			app.Name = item.Metadata.Name
		}
		apps = append(apps, app)
	}

	s.logger.Info("Fetched store apps", zap.Int("count", len(apps)))
	return apps, nil
}

// StoreAppVersion 商店应用版本（详情 / install 用）。
//
// ComposeTemplate 用 json:"-" 标记：永不序列化到 HTTP 响应，仅后端渲染持有，
// 避免向前端泄露 catalog 原文（CEO 裁决第4/5条）。
// ValuesSchema / DefaultValues 透传 edge-apiserver 原样 JSON，前端按需读取。
type StoreAppVersion struct {
	AppID   string `json:"appId"`
	Version string `json:"version"`
	// Runtime 权威运行时：存在 compose 模板 → compose；否则 kubernetes。
	Runtime              RuntimeKind `json:"runtime"`
	Installable          bool        `json:"installable"`
	NotInstallableReason string      `json:"notInstallableReason,omitempty"`

	// ComposeTemplate catalog 提供的 Compose 模板原文（仅 compose 包）。
	// 兼容读取 spec.composeTemplate（首选）与 spec.compose（别名）；edge-apiserver
	// 真实 ApplicationVersionSpec 当前无此字段，留空即视为 Kubernetes 包。
	ComposeTemplate string `json:"-"`

	// ValuesSchema 参数 UI/校验定义（透传 edge-apiserver valuesSchema 原样）。
	ValuesSchema json.RawMessage `json:"valuesSchema,omitempty"`
	// DefaultValues 参数默认值（透传 edge-apiserver values，map[string]apiextv1.JSON）。
	DefaultValues map[string]json.RawMessage `json:"defaultValues,omitempty"`
	// Catalog 来源标识（第三方 HTTP/Git catalog，与 StoreApp 对齐；edge 商店留空）。
	CatalogID   string `json:"catalogId,omitempty"`
	CatalogName string `json:"catalogName,omitempty"`
}

// storeVersionsResponse edge-apiserver /storeapps/{name}/versions 返回。
type storeVersionsResponse struct {
	Items []storeVersionItem `json:"items"`
}

type storeVersionItem struct {
	Spec   storeVersionSpec `json:"spec"`
	Status struct {
		ReviewPhase string `json:"reviewPhase"`
	} `json:"status"`
}

// storeVersionSpec 宽松解析 ApplicationVersionSpec：只取 devbox 关心的字段。
// composeTemplate/compose 为向后兼容扩展字段（真实类型暂无，留空）。
type storeVersionSpec struct {
	Version         string                     `json:"version"`
	ComposeTemplate string                     `json:"composeTemplate"`
	Compose         string                     `json:"compose"`
	ValuesSchema    json.RawMessage            `json:"valuesSchema"`
	Values          map[string]json.RawMessage `json:"values"`
}

// GetStoreAppVersion 从 catalog 重新获取指定应用的指定版本（可信源）。
//   - version 为空时取审核通过版本中语义最大者；否则精确匹配 spec.version。
//   - 仅返回 ReviewPhase=Approved 的版本。
//   - 存在 compose 模板 → Runtime=compose + Installable=true；否则 kubernetes。
//
// 该方法是 install 的可信数据源：前端不得传 compose 原文冒充 catalog（CEO 裁决第5条）。
func (s *StoreManager) GetStoreAppVersion(ctx context.Context, appID, version string) (StoreAppVersion, error) {
	if strings.TrimSpace(appID) == "" {
		return StoreAppVersion{}, fmt.Errorf("appId is required")
	}
	u := s.apiURL + "/oapis/app.theriseunion.io/v1alpha1/storeapps/" +
		url.PathEscape(appID) + "/versions?limit=1000"

	body, err := s.catalogGet(ctx, u)
	if err != nil {
		return StoreAppVersion{}, fmt.Errorf("fetch store versions: %w", err)
	}

	var data storeVersionsResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return StoreAppVersion{}, fmt.Errorf("decode store versions: %w", err)
	}

	// 选目标版本：精确匹配优先；否则取 approved 中最大版本；再否则首个 approved。
	var picked *storeVersionItem
	var approved []*storeVersionItem
	for i := range data.Items {
		it := &data.Items[i]
		if strings.EqualFold(it.Status.ReviewPhase, "Approved") {
			approved = append(approved, it)
		}
	}
	want := strings.TrimSpace(version)
	for _, it := range approved {
		if want != "" && it.Spec.Version == want {
			picked = it
			break
		}
	}
	if picked == nil && len(approved) > 0 {
		picked = maxVersionItem(approved)
	}
	if picked == nil {
		if want != "" {
			return StoreAppVersion{}, fmt.Errorf("approved version %q of %q not found", want, appID)
		}
		return StoreAppVersion{}, fmt.Errorf("no approved version of %q", appID)
	}

	return s.toStoreAppVersion(appID, picked), nil
}

// toStoreAppVersion 把 catalog 原始 item 映射为对外 StoreAppVersion（含 runtime 判定）。
func (s *StoreManager) toStoreAppVersion(appID string, it *storeVersionItem) StoreAppVersion {
	compose := strings.TrimSpace(it.Spec.ComposeTemplate)
	if compose == "" {
		compose = strings.TrimSpace(it.Spec.Compose)
	}
	ver := StoreAppVersion{
		AppID:           appID,
		Version:         it.Spec.Version,
		ComposeTemplate: compose,
		ValuesSchema:    it.Spec.ValuesSchema,
		DefaultValues:   it.Spec.Values,
	}
	if compose != "" {
		ver.Runtime = RuntimeCompose
		ver.Installable = true
	} else {
		ver.Runtime = RuntimeKubernetes
		ver.Installable = false
		ver.NotInstallableReason = "仅 Kubernetes 环境支持"
	}
	return ver
}

// maxVersionItem 返回版本号语义最大者（简单分段比较；格式不一致 fallback 字符串比较）。
func maxVersionItem(items []*storeVersionItem) *storeVersionItem {
	best := items[0]
	for _, it := range items[1:] {
		if compareVersionStrings(it.Spec.Version, best.Spec.Version) > 0 {
			best = it
		}
	}
	return best
}

// compareVersionStrings 语义版本比较，返 -1/0/1。数值段按数值比较、非数值段按字符串
// 回退，与前端 compareVersions（parseInt 优先）对齐：10.0.0 > 9.0.0，1.10.0 > 1.9.0。
func compareVersionStrings(a, b string) int {
	na := splitVersion(a)
	nb := splitVersion(b)
	for i := 0; i < len(na) || i < len(nb); i++ {
		x, y := "0", "0"
		if i < len(na) {
			x = na[i]
		}
		if i < len(nb) {
			y = nb[i]
		}
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		if xerr == nil && yerr == nil {
			if xn != yn {
				if xn < yn {
					return -1
				}
				return 1
			}
			continue
		}
		if c := strings.Compare(x, y); c != 0 {
			return c
		}
	}
	return 0
}

func splitVersion(s string) []string {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == '.' || r == '-' || r == '+'
	})
}

// catalogGet 执行带认证的 GET，返回受 maxCatalogBytes 限制的响应体。
// 非 200 返回错误（错误体同样受限并裁剪，避免回显敏感信息）。
func (s *StoreManager) catalogGet(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := readLimited(resp.Body, maxCatalogBytes)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		// 错误体裁剪，避免异常 apiserver 把超长 / 含密内容回显到 devbox 日志或前端。
		snippet := string(body)
		if len(snippet) > 512 {
			snippet = snippet[:512]
		}
		return nil, fmt.Errorf("catalog API returned %d: %s", resp.StatusCode, snippet)
	}
	return body, nil
}

// readLimited 最多读取 max 字节；超过即返回错误（防止超大响应）。
func readLimited(r io.Reader, max int64) ([]byte, error) {
	lr := io.LimitReader(r, max+1)
	b, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("catalog response exceeds %d bytes", max)
	}
	return b, nil
}

// GetAPIURL 返回 apiserver 地址
func (s *StoreManager) GetAPIURL() string { return s.apiURL }

// GetConsoleURL 返回 console 地址
func (s *StoreManager) GetConsoleURL() string { return s.consoleURL }

// GetToken 返回当前 token
func (s *StoreManager) GetToken() string { return s.token }

// --- Store install DTO（HTTP /store/install 请求 / 响应）---

// StoreInstallRequest 商店安装请求（前端提交）。
// Values 为用户填写的参数（含 password 字段）；compose 原文由后端从 catalog 取，前端不传。
type StoreInstallRequest struct {
	AppID   string         `json:"appId"`
	Version string         `json:"version"`
	Values  map[string]any `json:"values,omitempty"`
	// ConfirmRisky 表示用户已显式确认 confirmation 级运行权限；blocked 风险仍不可绕过。
	ConfirmRisky bool `json:"confirmRisky,omitempty"`
	// IdempotencyKey 可选；仅在调用方显式提供时启用跨请求幂等。
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
}

// StoreInstallResult 商店安装响应（202 + Task）。
// 前端用 TaskID 经 useTask 轮询 /api/v1/tasks/{taskId}。
type StoreInstallResult struct {
	TaskID   string `json:"taskId"`
	AppID    string `json:"appId"`
	Name     string `json:"name"`
	Revision int64  `json:"revision,omitempty"`
}

// StoreInstallFingerprint 生成参数与 secret 的单向摘要。用于请求哈希时保证 secret
// 轮换不会被误判为同一请求；摘要不泄露 secret 明文。
func StoreInstallFingerprint(params, secrets map[string]string) string {
	keys := make([]string, 0, len(params)+len(secrets))
	for k := range params {
		keys = append(keys, k)
	}
	for k := range secrets {
		if _, dup := params[k]; !dup {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{'='})
		if v, ok := secrets[k]; ok {
			h.Write([]byte(v))
		} else {
			h.Write([]byte(params[k]))
		}
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}
