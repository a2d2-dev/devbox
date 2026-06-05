package config

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

// Config 包含 OTA Agent 的全部配置
type Config struct {
	Device     DeviceConfig     `mapstructure:"device"`
	MQTT       MQTTConfig       `mapstructure:"mqtt"`
	Console    ConsoleConfig    `mapstructure:"console"`
	Kubernetes KubernetesConfig `mapstructure:"kubernetes"`
	Logging    LoggingConfig    `mapstructure:"logging"`
}

// KubernetesConfig K8s 连接配置
type KubernetesConfig struct {
	Kubeconfig   string `mapstructure:"kubeconfig"`    // kubeconfig 路径（空则 in-cluster）
	Namespace    string `mapstructure:"namespace"`     // 应用所在 namespace
	APIServerURL string `mapstructure:"apiserver_url"` // edge-apiserver 地址（应用商店）
}

// ConsoleConfig 本地控制台配置
type ConsoleConfig struct {
	Enabled            bool   `mapstructure:"enabled"`              // 是否启用本地控制台
	Port               int    `mapstructure:"port"`                 // HTTP 监听端口
	StaticDir          string `mapstructure:"static_dir"`           // 静态文件目录（空则使用 embed）
	ConsoleURL         string `mapstructure:"console_url"`          // edge-console 地址（用于图标代理）
	SupervisorSocket   string `mapstructure:"supervisor_socket"`    // supervisord Unix socket 路径
	SupervisorConfDir  string `mapstructure:"supervisor_conf_dir"`  // supervisor conf.d 目录
}

// DeviceConfig 设备配置
type DeviceConfig struct {
	Name string `mapstructure:"name"` // 设备名称（唯一标识符）
}

// MQTTConfig MQTT 连接配置
type MQTTConfig struct {
	Host     string      `mapstructure:"host"`      // NATS MQTT 服务器地址
	Port     int         `mapstructure:"port"`      // MQTT over TLS 端口（8883）
	CAFile   string      `mapstructure:"ca_file"`   // CA 证书路径
	ClientID string      `mapstructure:"client_id"` // MQTT Client ID（留空则自动生成）
	Auth     *AuthConfig `mapstructure:"auth"`      // 认证配置
}

// AuthConfig 认证配置
type AuthConfig struct {
	Type     string `mapstructure:"type"`     // 认证类型: "token" 或 "userpass"
	Token    string `mapstructure:"token"`    // Token认证（type=token时使用）
	Username string `mapstructure:"username"` // 用户名（type=userpass时使用）
	Password string `mapstructure:"password"` // 密码（type=userpass时使用）
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	Level string `mapstructure:"level"` // 日志级别: debug, info, warn, error
	File  string `mapstructure:"file"`  // 日志文件路径（留空则输出到 stdout）
}

// Load 从配置文件加载配置
// 优先级: 环境变量 > 配置文件 > 默认值
func Load(configFile string) (*Config, error) {
	v := viper.New()

	// 设置默认值
	setDefaults(v)

	// 读取配置文件
	if configFile != "" {
		v.SetConfigFile(configFile)
	} else {
		// 默认配置文件路径
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath("/etc/ota-agent/")
		v.AddConfigPath("$HOME/.ota-agent/")
		v.AddConfigPath(".")
	}

	// 环境变量绑定
	v.SetEnvPrefix("OTA")
	v.AutomaticEnv()

	// 显式绑定嵌套键的环境变量
	v.BindEnv("device.name", "OTA_DEVICE_NAME")
	v.BindEnv("mqtt.host", "OTA_MQTT_HOST")
	v.BindEnv("mqtt.port", "OTA_MQTT_PORT")
	v.BindEnv("mqtt.ca_file", "OTA_MQTT_CA_FILE")
	v.BindEnv("mqtt.client_id", "OTA_MQTT_CLIENT_ID")
	v.BindEnv("mqtt.auth.type", "OTA_MQTT_AUTH_TYPE")
	v.BindEnv("mqtt.auth.token", "OTA_MQTT_AUTH_TOKEN")
	v.BindEnv("mqtt.auth.username", "OTA_MQTT_AUTH_USERNAME")
	v.BindEnv("mqtt.auth.password", "OTA_MQTT_AUTH_PASSWORD")
	v.BindEnv("console.enabled", "OTA_CONSOLE_ENABLED")
	v.BindEnv("console.port", "OTA_CONSOLE_PORT")
	v.BindEnv("console.static_dir", "OTA_CONSOLE_STATIC_DIR")
	v.BindEnv("kubernetes.kubeconfig", "OTA_KUBECONFIG")
	v.BindEnv("kubernetes.namespace", "OTA_NAMESPACE")
	v.BindEnv("kubernetes.apiserver_url", "OTA_APISERVER_URL")
	v.BindEnv("logging.level", "OTA_LOGGING_LEVEL")
	v.BindEnv("logging.file", "OTA_LOGGING_FILE")

	// 读取配置文件（可选）
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		// 配置文件不存在时继续（使用默认值和环境变量）
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// 验证配置
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// setDefaults 设置默认配置
func setDefaults(v *viper.Viper) {
	// 设备配置
	hostname, _ := os.Hostname()
	v.SetDefault("device.name", hostname)

	// MQTT 配置
	v.SetDefault("mqtt.host", "localhost")
	v.SetDefault("mqtt.port", 8883)
	v.SetDefault("mqtt.ca_file", "")

	// 控制台配置
	v.SetDefault("console.enabled", true)
	v.SetDefault("console.port", 9090)
	v.SetDefault("console.static_dir", "")
	v.SetDefault("console.supervisor_socket", "/var/run/supervisor.sock")
	v.SetDefault("console.supervisor_conf_dir", "/etc/supervisor/conf.d")

	// K8s 配置
	v.SetDefault("kubernetes.kubeconfig", "")
	v.SetDefault("kubernetes.namespace", "default")

	// 日志配置
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.file", "")
}

// Validate 验证配置有效性
func (c *Config) Validate() error {
	if c.Device.Name == "" {
		return fmt.Errorf("device.name is required")
	}

	if c.MQTT.Host == "" {
		return fmt.Errorf("mqtt.host is required")
	}

	if c.MQTT.Port <= 0 || c.MQTT.Port > 65535 {
		return fmt.Errorf("mqtt.port must be between 1 and 65535")
	}

	// 验证 CA 证书文件存在
	if c.MQTT.CAFile != "" {
		if _, err := os.Stat(c.MQTT.CAFile); os.IsNotExist(err) {
			return fmt.Errorf("CA file not found: %s", c.MQTT.CAFile)
		}
	}

	// 验证认证配置
	if c.MQTT.Auth != nil {
		if c.MQTT.Auth.Type != "token" && c.MQTT.Auth.Type != "userpass" {
			return fmt.Errorf("mqtt.auth.type must be 'token' or 'userpass', got: %s", c.MQTT.Auth.Type)
		}

		if c.MQTT.Auth.Type == "token" && c.MQTT.Auth.Token == "" {
			return fmt.Errorf("mqtt.auth.token is required when type is 'token'")
		}

		if c.MQTT.Auth.Type == "userpass" {
			if c.MQTT.Auth.Username == "" {
				return fmt.Errorf("mqtt.auth.username is required when type is 'userpass'")
			}
			if c.MQTT.Auth.Password == "" {
				return fmt.Errorf("mqtt.auth.password is required when type is 'userpass'")
			}
		}
	}

	return nil
}

// GetMQTTBroker 获取 MQTT Broker URL
func (c *Config) GetMQTTBroker() string {
	// 如果提供了 CA 文件，使用 TLS；否则使用普通 TCP
	if c.MQTT.CAFile != "" {
		return fmt.Sprintf("tls://%s:%d", c.MQTT.Host, c.MQTT.Port)
	}
	return fmt.Sprintf("tcp://%s:%d", c.MQTT.Host, c.MQTT.Port)
}

// GetClientID 获取 MQTT Client ID
// 如果配置中未指定，则使用设备名称
func (c *Config) GetClientID() string {
	if c.MQTT.ClientID != "" {
		return c.MQTT.ClientID
	}
	return fmt.Sprintf("ota-agent-%s", c.Device.Name)
}
