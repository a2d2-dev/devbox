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
			name:    "default-like config is valid",
			config:  &Config{Console: ConsoleConfig{Enabled: true, Port: 9090}},
			wantErr: false,
		},
		{
			name:    "disabled console does not require port",
			config:  &Config{Console: ConsoleConfig{Enabled: false, Port: 0}},
			wantErr: false,
		},
		{
			name:    "enabled console rejects zero port",
			config:  &Config{Console: ConsoleConfig{Enabled: true, Port: 0}},
			wantErr: true,
			errMsg:  "console.port must be between 1 and 65535",
		},
		{
			name:    "enabled console rejects out-of-range port",
			config:  &Config{Console: ConsoleConfig{Enabled: true, Port: 70000}},
			wantErr: true,
			errMsg:  "console.port must be between 1 and 65535",
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

func TestLoadConfigWithDefaults(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	// Empty file -- everything from defaults.
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	require.NoError(t, err)

	assert.True(t, cfg.Console.Enabled)
	assert.Equal(t, 9090, cfg.Console.Port)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "default", cfg.Kubernetes.Namespace)
	assert.Equal(t, []string{"/data", "/var/lib"}, cfg.Compose.MigrationAllowedRoots)
}

func TestLoadConfigWithDockerMigrationAllowedRoots(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	_, err = tmpFile.Write([]byte("compose:\n  migration_allowed_roots:\n    - /srv/docker\n"))
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	cfg, err := Load(tmpFile.Name())
	require.NoError(t, err)
	assert.Equal(t, []string{"/srv/docker"}, cfg.Compose.MigrationAllowedRoots)
}

func TestLoadConfigWithEnv(t *testing.T) {
	os.Setenv("DEVBOX_CONSOLE_PORT", "18080")
	os.Setenv("DEVBOX_LOGGING_LEVEL", "debug")
	defer os.Unsetenv("DEVBOX_CONSOLE_PORT")
	defer os.Unsetenv("DEVBOX_LOGGING_LEVEL")

	cfg, err := Load("")
	require.NoError(t, err)

	assert.Equal(t, 18080, cfg.Console.Port)
	assert.Equal(t, "debug", cfg.Logging.Level)
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "invalid-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write([]byte("invalid: yaml: content:\n  - malformed"))
	require.NoError(t, err)
	tmpFile.Close()

	_, err = Load(tmpFile.Name())
	require.Error(t, err)
}
