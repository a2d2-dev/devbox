package apps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyDeploymentSettings(t *testing.T) {
	rendered, err := applyDeploymentSettings("services:\n  web:\n    image: nginx:1.27\n", &DeploymentSettings{
		DataPath: "/srv/my-app", DataTarget: "/data", CPULimit: "1.5", MemoryLimit: "512M", AutoStart: true,
	})
	require.NoError(t, err)
	assert.Contains(t, rendered, "restart: unless-stopped")
	assert.Contains(t, rendered, "/srv/my-app:/data")
	assert.Contains(t, rendered, `cpus: "1.5"`)
	assert.Contains(t, rendered, "memory: 512M")
	assert.Contains(t, rendered, "image: nginx:1.27")
}

func TestApplyDeploymentSettingsAutoStartOff(t *testing.T) {
	rendered, err := applyDeploymentSettings("services:\n  web:\n    image: nginx:1.27\n    restart: always\n", &DeploymentSettings{})
	require.NoError(t, err)
	assert.Contains(t, rendered, "restart: no")
}

func TestApplyDeploymentSettingsRejectsInvalidValues(t *testing.T) {
	base := "services:\n  web:\n    image: nginx:1.27\n"
	for _, settings := range []*DeploymentSettings{
		{DataPath: "relative", DataTarget: "/data"},
		{DataPath: "/srv/data", DataTarget: "relative"},
		{CPULimit: "$(bad)"},
		{MemoryLimit: "unlimited"},
	} {
		_, err := applyDeploymentSettings(base, settings)
		assert.Error(t, err)
	}
}
