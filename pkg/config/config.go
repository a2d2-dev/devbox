package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config 是 devbox 的运行时配置。
type Config struct {
	Console    ConsoleConfig    `mapstructure:"console"`
	Kubernetes KubernetesConfig `mapstructure:"kubernetes"`
	Compose    ComposeConfig    `mapstructure:"compose"`
	Auth       AuthConfig       `mapstructure:"auth"`
	Logging    LoggingConfig    `mapstructure:"logging"`
}

// AuthConfig 本地认证：单密码 + session token。
// Password 为空则不启用认证（所有请求直接放行）。
type AuthConfig struct {
	Password   string `mapstructure:"password"`
	SessionTTL int    `mapstructure:"session_ttl"` // 秒
}

// ConsoleConfig 本地控制台 HTTP 服务的配置。
type ConsoleConfig struct {
	Enabled            bool     `mapstructure:"enabled"`              // 是否启用本地控制台
	Port               int      `mapstructure:"port"`                 // HTTP 监听端口
	StaticDir          string   `mapstructure:"static_dir"`           // 静态文件目录（空则使用 embed）
	WorkDir            string   `mapstructure:"work_dir"`             // 文件浏览器工作区根目录；空则默认 /data
	ConsoleURL         string   `mapstructure:"console_url"`          // 外部 Console 地址（用于图标代理，可选）
	SupervisorSocket   string   `mapstructure:"supervisor_socket"`    // supervisord Unix socket 路径
	SupervisorConfDir  string   `mapstructure:"supervisor_conf_dir"`  // supervisor conf.d 目录
	LinksPath          string   `mapstructure:"links_path"`           // 服务导航 YAML 路径；空 = /etc/devbox/links.yaml
	BrowserDataPath    string   `mapstructure:"browser_data_path"`    // 浏览器书签/历史 JSON 路径；空 = /etc/devbox/browser.json
	BrowserInsecureTLS bool     `mapstructure:"browser_insecure_tls"` // 浏览器代理是否跳过远端 TLS 校验（内网自签证书）
	TrustedProxies     []string `mapstructure:"trusted_proxies"`      // 可解析 X-Forwarded-For 的可信反向代理 IP/CIDR
}

// KubernetesConfig 可选的 K8s 集成（应用市场 / Pod 管理）。
// 不设置则禁用 K8s 相关功能。
type KubernetesConfig struct {
	Kubeconfig   string `mapstructure:"kubeconfig"`    // kubeconfig 路径（空则尝试 in-cluster）
	Namespace    string `mapstructure:"namespace"`     // 默认 namespace
	APIServerURL string `mapstructure:"apiserver_url"` // 应用商店 API 地址（可选）
}

// ComposeConfig Docker Compose 应用管理（Issue #2）。
// Compose 与 K8s 并列；Compose 默认启用，Docker 不可用时仅该运行时降级，不影响 K8s。
type ComposeConfig struct {
	Enabled      bool                  `mapstructure:"enabled"`       // 默认 true
	DataDir      string                `mapstructure:"data_dir"`      // 数据根（apps.db + apps/<id>）；默认 /var/lib/devbox
	DockerSocket string                `mapstructure:"docker_socket"` // Docker daemon unix socket；默认 /var/run/docker.sock
	Catalogs     []CatalogSourceConfig `mapstructure:"catalogs"`      // 第三方 HTTP/Git catalog source（Issue #2 阶段4 扩展）
	CatalogPoll  int                   `mapstructure:"catalog_poll"`  // catalog 周期同步间隔（秒）；0=不同步，<=0 关闭周期刷新
}

// CatalogSourceConfig 第三方 Docker Compose catalog source 配置。
//
// kind=http: url = manifest 所在 base URL（取 <base>/catalog.json 与 <base>/<compose 相对路径>）。
// kind=git: url = devbox/v1 Git 仓库；kind=1panel: 原生 1Panel AppStore 仓库。
//
//	ref=分支/tag（git 默认 main；1panel 留空使用远端 HEAD）；path=仓库内子目录（默认根）。
//	clone，拒绝 file/ssh/local。
//
// 安全：URL 仅 https（http 仅 localhost/内网或 insecure=true）。token 为只读访问令牌，
// 视为 secret，绝不入日志/审计/revision。
type CatalogSourceConfig struct {
	ID       string `mapstructure:"id"`   // 稳定标识（来源筛选与 install 路由）；空则按 url 派生
	Name     string `mapstructure:"name"` // 人读来源名；空用 ID
	Kind     string `mapstructure:"kind"` // "http" | "git" | "1panel"
	URL      string `mapstructure:"url"`
	Platform string `mapstructure:"platform"` // git: github|gitlab
	Host     string `mapstructure:"host"`     // gitlab host
	Ref      string `mapstructure:"ref"`      // git: 分支/sha
	Path     string `mapstructure:"path"`     // 仓库内子目录
	Token    string `mapstructure:"token"`    // 只读 token（secret）
	Insecure bool   `mapstructure:"insecure"` // 允许 http（仅 localhost 类地址生效）
}

// LoggingConfig 日志配置。
type LoggingConfig struct {
	Level string `mapstructure:"level"` // debug / info / warn / error
	File  string `mapstructure:"file"`  // 留空则输出到 stdout
}

// Load 从配置文件加载 devbox 配置。
// 优先级：环境变量 (DEVBOX_*) > 配置文件 > 默认值。
// configFile 为空时按顺序在 /etc/devbox/、$HOME/.devbox/、当前目录查找 config.yaml。
func Load(configFile string) (*Config, error) {
	v := viper.New()

	setDefaults(v)

	if configFile != "" {
		v.SetConfigFile(configFile)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath("/etc/devbox/")
		v.AddConfigPath("$HOME/.devbox/")
		v.AddConfigPath(".")
	}

	v.SetEnvPrefix("DEVBOX")
	v.AutomaticEnv()

	v.BindEnv("console.enabled", "DEVBOX_CONSOLE_ENABLED")
	v.BindEnv("console.port", "DEVBOX_CONSOLE_PORT")
	v.BindEnv("console.static_dir", "DEVBOX_CONSOLE_STATIC_DIR")
	v.BindEnv("console.work_dir", "DEVBOX_CONSOLE_WORK_DIR")
	v.BindEnv("console.console_url", "DEVBOX_CONSOLE_URL")
	v.BindEnv("console.supervisor_socket", "DEVBOX_SUPERVISOR_SOCKET")
	v.BindEnv("console.supervisor_conf_dir", "DEVBOX_SUPERVISOR_CONF_DIR")
	v.BindEnv("console.browser_data_path", "DEVBOX_CONSOLE_BROWSER_DATA_PATH")
	v.BindEnv("console.browser_insecure_tls", "DEVBOX_CONSOLE_BROWSER_INSECURE_TLS")
	v.BindEnv("console.trusted_proxies", "DEVBOX_CONSOLE_TRUSTED_PROXIES")
	v.BindEnv("kubernetes.kubeconfig", "DEVBOX_KUBECONFIG")
	v.BindEnv("kubernetes.namespace", "DEVBOX_NAMESPACE")
	v.BindEnv("kubernetes.apiserver_url", "DEVBOX_APISERVER_URL")
	v.BindEnv("compose.enabled", "DEVBOX_COMPOSE_ENABLED")
	v.BindEnv("compose.data_dir", "DEVBOX_COMPOSE_DATA_DIR")
	v.BindEnv("compose.docker_socket", "DEVBOX_COMPOSE_DOCKER_SOCKET")
	v.BindEnv("compose.catalog_poll", "DEVBOX_COMPOSE_CATALOG_POLL")
	v.BindEnv("auth.password", "DEVBOX_AUTH_PASSWORD")
	v.BindEnv("auth.session_ttl", "DEVBOX_AUTH_SESSION_TTL")
	v.BindEnv("logging.level", "DEVBOX_LOGGING_LEVEL")
	v.BindEnv("logging.file", "DEVBOX_LOGGING_FILE")

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("console.enabled", true)
	v.SetDefault("console.port", 9090)
	v.SetDefault("console.static_dir", "")
	v.SetDefault("console.supervisor_socket", "/var/run/supervisor.sock")
	v.SetDefault("console.supervisor_conf_dir", "/etc/supervisor/conf.d")
	v.SetDefault("console.links_path", "/etc/devbox/links.yaml")
	v.SetDefault("console.browser_data_path", "/etc/devbox/browser.json")
	v.SetDefault("console.browser_insecure_tls", false)

	v.SetDefault("kubernetes.kubeconfig", "")
	v.SetDefault("kubernetes.namespace", "default")

	v.SetDefault("compose.enabled", true)
	v.SetDefault("compose.data_dir", "/var/lib/devbox")
	v.SetDefault("compose.docker_socket", "/var/run/docker.sock")
	// catalog 周期同步间隔（秒）。0=仅启动时同步一次 + 显式 refresh，不做周期刷新。
	v.SetDefault("compose.catalog_poll", 300)

	v.SetDefault("auth.password", "")
	v.SetDefault("auth.session_ttl", 86400)

	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.file", "")
}

// Validate 校验配置。
func (c *Config) Validate() error {
	if c.Console.Enabled {
		if c.Console.Port <= 0 || c.Console.Port > 65535 {
			return fmt.Errorf("console.port must be between 1 and 65535")
		}
	}
	for i, cat := range c.Compose.Catalogs {
		if cat.Kind != "http" && cat.Kind != "git" && cat.Kind != "1panel" {
			return fmt.Errorf("compose.catalogs[%d]: kind must be http, git or 1panel (got %q)", i, cat.Kind)
		}
		if cat.URL == "" {
			return fmt.Errorf("compose.catalogs[%d]: url is required", i)
		}
	}
	return nil
}
