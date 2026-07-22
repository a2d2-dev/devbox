package apps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const invCompose = `services:
  web:
    image: nginx:1.27
    environment:
      PUBLIC_ENV: "1"
      DB_PASSWORD: ${DB_PASSWORD}
    volumes:
      - data:/var/lib/data
      - /host/path:/host
      - /var/run/docker.sock:/var/run/docker.sock
  db:
    volumes:
      - "shared:/data"

volumes:
  data:
  shared:
    external: true
  ext2:
    name: external-volume
    external: true
`

const invEnvFile = `PUBLIC_ENV=1
DB_PASSWORD=hunter2
UNUSED_KEY=whatever
# a comment
`

func TestAnalyzeStorage_Classification(t *testing.T) {
	vols, _, err := analyzeStorage(invCompose, invEnvFile)
	require.NoError(t, err)

	// 期望分类：managed(data, shared→其实 external), bind(/host/path), socket(docker.sock),
	//   external(ext2)。注：shared 在顶层声明 external:true → external（即便 service 引用）。
	bySrc := map[string]VolumeInfo{}
	for _, v := range vols {
		bySrc[v.Source] = v
	}
	assert.Equal(t, VolumeManaged, bySrc["data"].Kind, "data is managed named volume")
	assert.True(t, bySrc["data"].Managed)
	assert.True(t, bySrc["data"].Deletable)

	assert.Equal(t, VolumeBind, bySrc["/host/path"].Kind, "/host/path is a bind mount")

	assert.Equal(t, VolumeSocket, bySrc["/var/run/docker.sock"].Kind, "docker.sock is a privileged socket")

	// shared 被顶层声明为 external → external（service 引用不影响）。
	assert.Equal(t, VolumeExternal, bySrc["shared"].Kind, "shared declared external at top level")
	assert.True(t, bySrc["shared"].External)
	assert.False(t, bySrc["shared"].Deletable, "external never deletable")
}

func TestAnalyzeStorage_ExternalNamedVolume(t *testing.T) {
	vols, _, err := analyzeStorage(invCompose, "")
	require.NoError(t, err)
	for _, v := range vols {
		if v.Source == "external-volume" || v.Source == "ext2" {
			assert.Equal(t, VolumeExternal, v.Kind)
		}
	}
}

func TestAnalyzeStorage_EnvMetadataNoValues(t *testing.T) {
	_, vars, err := analyzeStorage(invCompose, invEnvFile)
	require.NoError(t, err)

	byKey := map[string]EnvVarInfo{}
	for _, v := range vars {
		byKey[v.Key] = v
	}

	// PUBLIC_ENV：environment 显式设 + .env 提供 → configured。
	assert.True(t, byKey["PUBLIC_ENV"].Configured)
	assert.Equal(t, "text", byKey["PUBLIC_ENV"].Type)

	// DB_PASSWORD：${} 引用 + .env 提供 → configured；secrety key → password。
	assert.True(t, byKey["DB_PASSWORD"].Configured)
	assert.Equal(t, "password", byKey["DB_PASSWORD"].Type)

	// UNUSED_KEY：.env 提供但 compose 未引用 → configured, not required。
	assert.True(t, byKey["UNUSED_KEY"].Configured)
	assert.False(t, byKey["UNUSED_KEY"].Required)

	// 值绝不出现。
	for _, v := range vars {
		assert.Equal(t, EnvVarInfo{Key: v.Key, Configured: v.Configured, Type: v.Type, Required: v.Required}, v)
	}
}

func TestAnalyzeStorage_RequiredMissing(t *testing.T) {
	compose := "services:\n  web:\n    image: nginx\n    environment:\n      MISSING: ${MISSING}\n"
	_, vars, err := analyzeStorage(compose, "")
	require.NoError(t, err)
	for _, v := range vars {
		if v.Key == "MISSING" {
			assert.True(t, v.Required, "${MISSING} referenced but not in .env → required")
			assert.False(t, v.Configured)
		}
	}
}

func TestAnalyzeStorage_InvalidYAML(t *testing.T) {
	_, _, err := analyzeStorage("::: not yaml :::", "")
	require.Error(t, err)
}

func TestAnalyzeStorage_Empty(t *testing.T) {
	vols, vars, err := analyzeStorage("services:\n  web:\n    image: nginx\n", "")
	require.NoError(t, err)
	assert.Empty(t, vols)
	assert.Empty(t, vars)
}

// RemovePreview 由 controller 层组合；这里只覆盖 analyzeStorage 输出对预览的支撑：
// external/bind 永远在 willKeep，managed 受 purge 控制（在 controller 测试中验证）。
func TestAnalyzeStorage_ManagedOnlyDeletable(t *testing.T) {
	vols, _, err := analyzeStorage(invCompose, "")
	require.NoError(t, err)
	for _, v := range vols {
		switch v.Kind {
		case VolumeManaged:
			assert.True(t, v.Deletable)
		case VolumeExternal, VolumeBind, VolumeSocket:
			assert.False(t, v.Deletable)
		}
	}
}
