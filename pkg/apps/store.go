package apps

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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

	mgr := &StoreManager{
		apiURL:     cfg.APIServerURL,
		consoleURL: cfg.ConsoleURL,
		logger:     logger,
		client:     httpClient,
		token:      restConfig.BearerToken,
	}

	return mgr, nil
}

// storeAppsResponse edge-apiserver 返回的 storeapps 列表
type storeAppsResponse struct {
	Items []storeAppItem `json:"items"`
}

type storeAppItem struct {
	Metadata struct {
		Name        string            `json:"name"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Status struct {
		LatestVersion string `json:"latestVersion"`
		VersionCount  int    `json:"versionCount"`
	} `json:"status"`
}

// ListStoreApps 从 edge-apiserver 获取应用商店列表
func (s *StoreManager) ListStoreApps(ctx context.Context) ([]StoreApp, error) {
	url := s.apiURL + "/oapis/app.theriseunion.io/v1alpha1/storeapps?limit=1000"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch storeapps: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("storeapps API returned %d: %s", resp.StatusCode, string(body))
	}

	var data storeAppsResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode storeapps: %w", err)
	}

	apps := make([]StoreApp, 0, len(data.Items))
	for _, item := range data.Items {
		app := StoreApp{
			ID:           item.Metadata.Name,
			Name:         item.Metadata.Annotations["theriseunion.io/alias-name"],
			Category:     item.Metadata.Labels["app.theriseunion.io/category"],
			Description:  item.Metadata.Annotations["theriseunion.io/description"],
			Icon:         item.Metadata.Annotations["app.theriseunion.io/icon"],
			Provider:     item.Metadata.Annotations["app.theriseunion.io/provider"],
			Version:      item.Status.LatestVersion,
			VersionCount: item.Status.VersionCount,
		}
		if app.Name == "" {
			app.Name = item.Metadata.Name
		}
		apps = append(apps, app)
	}

	s.logger.Info("Fetched store apps", zap.Int("count", len(apps)))
	return apps, nil
}

// GetAPIURL 返回 apiserver 地址
func (s *StoreManager) GetAPIURL() string { return s.apiURL }

// GetConsoleURL 返回 console 地址
func (s *StoreManager) GetConsoleURL() string { return s.consoleURL }

// GetToken 返回当前 token
func (s *StoreManager) GetToken() string { return s.token }
