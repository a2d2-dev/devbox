//go:build unit
// +build unit

package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &Config{
				Device: DeviceConfig{Name: "test-device"},
				MQTT: MQTTConfig{
					Host:   "localhost",
					Port:   8883,
					CAFile: "",
				},
			},
			wantErr: false,
		},
		{
			name: "missing device name",
			config: &Config{
				Device: DeviceConfig{Name: ""},
				MQTT: MQTTConfig{
					Host: "localhost",
					Port: 8883,
				},
			},
			wantErr: true,
			errMsg:  "device.name is required",
		},
		{
			name: "missing mqtt host",
			config: &Config{
				Device: DeviceConfig{Name: "test-device"},
				MQTT: MQTTConfig{
					Host: "",
					Port: 8883,
				},
			},
			wantErr: true,
			errMsg:  "mqtt.host is required",
		},
		{
			name: "invalid port - zero",
			config: &Config{
				Device: DeviceConfig{Name: "test-device"},
				MQTT: MQTTConfig{
					Host: "localhost",
					Port: 0,
				},
			},
			wantErr: true,
			errMsg:  "mqtt.port must be between 1 and 65535",
		},
		{
			name: "invalid port - too large",
			config: &Config{
				Device: DeviceConfig{Name: "test-device"},
				MQTT: MQTTConfig{
					Host: "localhost",
					Port: 70000,
				},
			},
			wantErr: true,
			errMsg:  "mqtt.port must be between 1 and 65535",
		},
		{
			name: "valid port range",
			config: &Config{
				Device: DeviceConfig{Name: "test-device"},
				MQTT: MQTTConfig{
					Host: "localhost",
					Port: 1883,
				},
			},
			wantErr: false,
		},
		{
			name: "CA file not found",
			config: &Config{
				Device: DeviceConfig{Name: "test-device"},
				MQTT: MQTTConfig{
					Host:   "localhost",
					Port:   8883,
					CAFile: "/nonexistent/ca.crt",
				},
			},
			wantErr: true,
			errMsg:  "CA file not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetMQTTBroker(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   string
	}{
		{
			name: "standard port with TLS",
			config: &Config{
				MQTT: MQTTConfig{
					Host:   "mqtt.example.com",
					Port:   8883,
					CAFile: "/path/to/ca.crt",
				},
			},
			want: "tls://mqtt.example.com:8883",
		},
		{
			name: "localhost with TLS",
			config: &Config{
				MQTT: MQTTConfig{
					Host:   "localhost",
					Port:   8883,
					CAFile: "/path/to/ca.crt",
				},
			},
			want: "tls://localhost:8883",
		},
		{
			name: "IP address with TLS",
			config: &Config{
				MQTT: MQTTConfig{
					Host:   "192.168.1.100",
					Port:   1883,
					CAFile: "/path/to/ca.crt",
				},
			},
			want: "tls://192.168.1.100:1883",
		},
		{
			name: "no TLS (no CA file)",
			config: &Config{
				MQTT: MQTTConfig{
					Host:   "localhost",
					Port:   1883,
					CAFile: "",
				},
			},
			want: "tcp://localhost:1883",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetMQTTBroker()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetClientID(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   string
	}{
		{
			name: "explicit client ID",
			config: &Config{
				Device: DeviceConfig{Name: "device-001"},
				MQTT: MQTTConfig{
					ClientID: "custom-client-id",
				},
			},
			want: "custom-client-id",
		},
		{
			name: "generated from device name",
			config: &Config{
				Device: DeviceConfig{Name: "device-002"},
				MQTT: MQTTConfig{
					ClientID: "",
				},
			},
			want: "ota-agent-device-002",
		},
		{
			name: "complex device name",
			config: &Config{
				Device: DeviceConfig{Name: "edge-node-123"},
				MQTT: MQTTConfig{
					ClientID: "",
				},
			},
			want: "ota-agent-edge-node-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetClientID()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadConfigWithDefaults(t *testing.T) {
	// 创建临时配置文件
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	// 最小配置（依赖默认值），明确设置 ca_file 为空
	configContent := `
device:
  name: test-device
mqtt:
  host: mqtt.example.com
  ca_file: ""
`
	_, err = tmpFile.Write([]byte(configContent))
	require.NoError(t, err)
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	require.NoError(t, err)

	// 验证加载的配置
	assert.Equal(t, "test-device", cfg.Device.Name)
	assert.Equal(t, "mqtt.example.com", cfg.MQTT.Host)
	assert.Equal(t, 8883, cfg.MQTT.Port)       // 默认值
	assert.Equal(t, "info", cfg.Logging.Level) // 默认值
	assert.Empty(t, cfg.MQTT.CAFile)           // CA 文件设置为空
}

func TestLoadConfigWithEnv(t *testing.T) {
	// 创建临时 CA 文件
	tmpCA, err := os.CreateTemp("", "test-ca-*.crt")
	require.NoError(t, err)
	defer os.Remove(tmpCA.Name())
	tmpCA.WriteString("fake CA certificate for testing")
	tmpCA.Close()

	// 设置环境变量（在加载配置前）
	os.Setenv("OTA_DEVICE_NAME", "env-device")
	os.Setenv("OTA_MQTT_HOST", "env.mqtt.com")
	os.Setenv("OTA_MQTT_PORT", "1883")
	os.Setenv("OTA_MQTT_CA_FILE", tmpCA.Name())
	defer os.Unsetenv("OTA_DEVICE_NAME")
	defer os.Unsetenv("OTA_MQTT_HOST")
	defer os.Unsetenv("OTA_MQTT_PORT")
	defer os.Unsetenv("OTA_MQTT_CA_FILE")

	// 不提供配置文件，仅使用环境变量和默认值
	cfg, err := Load("")
	require.NoError(t, err)

	// 验证环境变量覆盖默认值
	assert.Equal(t, "env-device", cfg.Device.Name)
	assert.Equal(t, "env.mqtt.com", cfg.MQTT.Host)
	assert.Equal(t, 1883, cfg.MQTT.Port)
	assert.Equal(t, tmpCA.Name(), cfg.MQTT.CAFile)
}

func TestLoadConfigFileNotFound(t *testing.T) {
	// 创建临时配置文件
	tmpFile, err := os.CreateTemp("", "minimal-config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	// 写入最小配置
	configContent := `
device:
  name: test-device
mqtt:
  host: localhost
  ca_file: ""
`
	_, err = tmpFile.Write([]byte(configContent))
	require.NoError(t, err)
	tmpFile.Close()

	// 测试：使用有效的配置文件
	cfg, err := Load(tmpFile.Name())

	// 应该成功加载
	require.NoError(t, err)
	assert.NotNil(t, cfg)

	// 验证配置内容
	assert.Equal(t, "test-device", cfg.Device.Name)
	assert.Equal(t, "localhost", cfg.MQTT.Host)
	assert.Equal(t, 8883, cfg.MQTT.Port)
	assert.Empty(t, cfg.MQTT.CAFile)
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	// 创建无效的 YAML 文件
	tmpFile, err := os.CreateTemp("", "invalid-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write([]byte("invalid: yaml: content:\n  - malformed"))
	require.NoError(t, err)
	tmpFile.Close()

	_, err = Load(tmpFile.Name())

	// 应该返回解析错误
	require.Error(t, err)
}

func TestAuthConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid token authentication",
			config: &Config{
				Device: DeviceConfig{Name: "test-device"},
				MQTT: MQTTConfig{
					Host: "localhost",
					Port: 8883,
					Auth: &AuthConfig{
						Type:  "token",
						Token: "test-token-123",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid userpass authentication",
			config: &Config{
				Device: DeviceConfig{Name: "test-device"},
				MQTT: MQTTConfig{
					Host: "localhost",
					Port: 8883,
					Auth: &AuthConfig{
						Type:     "userpass",
						Username: "device-001",
						Password: "secret-password",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "no authentication (backward compatible)",
			config: &Config{
				Device: DeviceConfig{Name: "test-device"},
				MQTT: MQTTConfig{
					Host: "localhost",
					Port: 8883,
					Auth: nil,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid auth type",
			config: &Config{
				Device: DeviceConfig{Name: "test-device"},
				MQTT: MQTTConfig{
					Host: "localhost",
					Port: 8883,
					Auth: &AuthConfig{
						Type:  "invalid",
						Token: "test-token",
					},
				},
			},
			wantErr: true,
			errMsg:  "mqtt.auth.type must be 'token' or 'userpass'",
		},
		{
			name: "token auth missing token",
			config: &Config{
				Device: DeviceConfig{Name: "test-device"},
				MQTT: MQTTConfig{
					Host: "localhost",
					Port: 8883,
					Auth: &AuthConfig{
						Type:  "token",
						Token: "",
					},
				},
			},
			wantErr: true,
			errMsg:  "mqtt.auth.token is required when type is 'token'",
		},
		{
			name: "userpass auth missing username",
			config: &Config{
				Device: DeviceConfig{Name: "test-device"},
				MQTT: MQTTConfig{
					Host: "localhost",
					Port: 8883,
					Auth: &AuthConfig{
						Type:     "userpass",
						Username: "",
						Password: "password",
					},
				},
			},
			wantErr: true,
			errMsg:  "mqtt.auth.username is required when type is 'userpass'",
		},
		{
			name: "userpass auth missing password",
			config: &Config{
				Device: DeviceConfig{Name: "test-device"},
				MQTT: MQTTConfig{
					Host: "localhost",
					Port: 8883,
					Auth: &AuthConfig{
						Type:     "userpass",
						Username: "user",
						Password: "",
					},
				},
			},
			wantErr: true,
			errMsg:  "mqtt.auth.password is required when type is 'userpass'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLoadConfigWithAuth(t *testing.T) {
	tests := []struct {
		name          string
		configContent string
		wantAuthType  string
		wantToken     string
		wantUsername  string
		wantPassword  string
	}{
		{
			name: "token authentication",
			configContent: `
device:
  name: test-device
mqtt:
  host: localhost
  port: 8883
  ca_file: ""
  auth:
    type: token
    token: my-secret-token
`,
			wantAuthType: "token",
			wantToken:    "my-secret-token",
		},
		{
			name: "userpass authentication",
			configContent: `
device:
  name: test-device
mqtt:
  host: localhost
  port: 8883
  ca_file: ""
  auth:
    type: userpass
    username: device-001
    password: device-password
`,
			wantAuthType: "userpass",
			wantUsername: "device-001",
			wantPassword: "device-password",
		},
		{
			name: "no authentication",
			configContent: `
device:
  name: test-device
mqtt:
  host: localhost
  ca_file: ""
`,
			wantAuthType: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建临时配置文件
			tmpFile, err := os.CreateTemp("", "auth-config-*.yaml")
			require.NoError(t, err)
			defer os.Remove(tmpFile.Name())

			_, err = tmpFile.Write([]byte(tt.configContent))
			require.NoError(t, err)
			tmpFile.Close()

			cfg, err := Load(tmpFile.Name())
			require.NoError(t, err)

			if tt.wantAuthType == "" {
				assert.Nil(t, cfg.MQTT.Auth)
			} else {
				require.NotNil(t, cfg.MQTT.Auth)
				assert.Equal(t, tt.wantAuthType, cfg.MQTT.Auth.Type)

				if tt.wantAuthType == "token" {
					assert.Equal(t, tt.wantToken, cfg.MQTT.Auth.Token)
				} else if tt.wantAuthType == "userpass" {
					assert.Equal(t, tt.wantUsername, cfg.MQTT.Auth.Username)
					assert.Equal(t, tt.wantPassword, cfg.MQTT.Auth.Password)
				}
			}
		})
	}
}

func TestLoadConfigWithAuthEnv(t *testing.T) {
	// Create temporary CA file to satisfy validation
	tmpCA, err := os.CreateTemp("", "test-ca-*.crt")
	require.NoError(t, err)
	defer os.Remove(tmpCA.Name())
	tmpCA.WriteString("fake CA certificate for testing")
	tmpCA.Close()

	// Set authentication environment variables
	os.Setenv("OTA_DEVICE_NAME", "env-device")
	os.Setenv("OTA_MQTT_HOST", "localhost")
	os.Setenv("OTA_MQTT_CA_FILE", tmpCA.Name())
	os.Setenv("OTA_MQTT_AUTH_TYPE", "token")
	os.Setenv("OTA_MQTT_AUTH_TOKEN", "env-token-123")
	defer os.Unsetenv("OTA_DEVICE_NAME")
	defer os.Unsetenv("OTA_MQTT_HOST")
	defer os.Unsetenv("OTA_MQTT_CA_FILE")
	defer os.Unsetenv("OTA_MQTT_AUTH_TYPE")
	defer os.Unsetenv("OTA_MQTT_AUTH_TOKEN")

	// Load without config file (rely on env vars)
	cfg, err := Load("")
	require.NoError(t, err)

	// Verify auth config from environment variables
	require.NotNil(t, cfg.MQTT.Auth)
	assert.Equal(t, "token", cfg.MQTT.Auth.Type)
	assert.Equal(t, "env-token-123", cfg.MQTT.Auth.Token)
}
