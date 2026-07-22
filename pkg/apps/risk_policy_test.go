package apps

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 商店/catalog 包使用 latest/main 可变标签 → 阻断（要求 5）。
func TestApply_StoreMutableTagBlocked(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	ctrl, _, _, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})

	_, err := ctrl.Apply(context.Background(), DesiredApplication{
		ID:             "ghost",
		Name:           "ghost",
		Source:         ApplicationSource{Kind: SourceStore, StoreID: "ghost", Version: "1.0"},
		ComposeContent: "services:\n  web:\n    image: nginx:latest\n",
	}, ApplyOptions{Actor: "t"})
	ae, ok := AsError(err)
	require.True(t, ok, "expected domain error")
	assert.Equal(t, ErrKindRiskBlocked, ae.Kind)
}

func TestApply_CatalogMutableTagBlocked(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	ctrl, _, _, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})

	_, err := ctrl.Apply(context.Background(), DesiredApplication{
		ID:             "ghost",
		Name:           "ghost",
		Source:         ApplicationSource{Kind: SourceCatalog, StoreID: "ghost", CatalogID: "src1", Version: "1.0"},
		ComposeContent: "services:\n  web:\n    image: ghost:main\n",
	}, ApplyOptions{Actor: "t"})
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, ErrKindRiskBlocked, ae.Kind)
}

// 商店包固定版本标签 → 允许。
func TestApply_StorePinnedTagAllowed(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	ctrl, _, _, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})

	_, err := ctrl.Apply(context.Background(), DesiredApplication{
		ID:             "ghost",
		Name:           "ghost",
		Source:         ApplicationSource{Kind: SourceStore, StoreID: "ghost", Version: "1.0"},
		ComposeContent: "services:\n  web:\n    image: nginx:1.27\n",
	}, ApplyOptions{Actor: "t"})
	assert.NoError(t, err)
}

// inline 来源使用 latest → 仅 warning，不阻断（保留人工选择）。
func TestApply_InlineMutableTagAllowed(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	ctrl, _, _, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})

	_, err := ctrl.Apply(context.Background(), DesiredApplication{
		ID:             "myapp",
		Name:           "myapp",
		Source:         ApplicationSource{Kind: SourceInline},
		ComposeContent: "services:\n  web:\n    image: nginx:latest\n",
	}, ApplyOptions{Actor: "t"})
	assert.NoError(t, err)
}

// 商店包 blocked 风险（privileged）→ 拒绝，不可 override（要求 5）。
func TestApply_StorePrivilegedBlockedNoOverride(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	ctrl, _, _, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})

	_, err := ctrl.Apply(context.Background(), DesiredApplication{
		ID:             "bad",
		Name:           "bad",
		Source:         ApplicationSource{Kind: SourceStore, StoreID: "bad"},
		ComposeContent: "services:\n  web:\n    image: nginx:1.27\n    privileged: true\n",
	}, ApplyOptions{Actor: "t", AllowRiskyConfirmation: true}) // 即便显式确认也拒绝
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, ErrKindRiskBlocked, ae.Kind)
}

// --- 详情方法（要求 6/7）---

const detailCompose = `services:
  web:
    image: nginx:1.27
    environment:
      DB_PASSWORD: ${DB_PASSWORD}
    volumes:
      - data:/var/lib/data
      - /host/path:/host
      - shared:/shared
volumes:
  data:
  shared:
    external: true
`

func applyDetailApp(t *testing.T) Controller {
	compose := &fakeAdapter{kind: RuntimeCompose}
	ctrl, _, _, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	_, err := ctrl.Apply(context.Background(), DesiredApplication{
		ID:             "ghost",
		Name:           "ghost",
		Source:         ApplicationSource{Kind: SourceStore, StoreID: "ghost", Version: "1.0"},
		ComposeContent: detailCompose,
		Secrets:        map[string]string{"DB_PASSWORD": "hunter2"},
	}, ApplyOptions{Actor: "t", AllowRiskyConfirmation: true})
	require.NoError(t, err)
	return ctrl
}

func TestController_StorageInventory(t *testing.T) {
	ctrl := applyDetailApp(t)
	inv, err := ctrl.StorageInventory(context.Background(), "ghost")
	require.NoError(t, err)
	assert.Equal(t, "ghost", inv.AppID)
	assert.NotEmpty(t, inv.ManagedDataDir)

	kinds := map[VolumeKind]bool{}
	for _, v := range inv.Volumes {
		kinds[v.Kind] = true
	}
	assert.True(t, kinds[VolumeManaged], "managed volume present")
	assert.True(t, kinds[VolumeBind], "bind present")
}

func TestController_EnvInventoryNoValues(t *testing.T) {
	ctrl := applyDetailApp(t)
	inv, err := ctrl.EnvInventory(context.Background(), "ghost")
	require.NoError(t, err)
	// DB_PASSWORD 经 ${} 引用 + .env 提供 → configured, password。
	var dbpw *EnvVarInfo
	for i := range inv.Vars {
		if inv.Vars[i].Key == "DB_PASSWORD" {
			dbpw = &inv.Vars[i]
		}
	}
	require.NotNil(t, dbpw)
	assert.True(t, dbpw.Configured)
	assert.Equal(t, "password", dbpw.Type)
}

func TestController_RemovePreviewDefaultKeepsData(t *testing.T) {
	ctrl := applyDetailApp(t)
	pre, err := ctrl.RemovePreview(context.Background(), "ghost", false)
	require.NoError(t, err)
	assert.False(t, pre.Purge)
	// 默认保留：managed volume / data dir 在 willKeep。
	joined := joinStrs(pre.WillKeep)
	assert.Contains(t, joined, "保留数据")
	assert.Contains(t, joined, "managed data dir")
	// willDelete 仅容器/网络。
	assert.Contains(t, joinStrs(pre.WillDelete), "containers & networks")
}

func TestController_RemovePreviewPurgeDeletesManaged(t *testing.T) {
	ctrl := applyDetailApp(t)
	pre, err := ctrl.RemovePreview(context.Background(), "ghost", true)
	require.NoError(t, err)
	assert.True(t, pre.Purge)
	del := joinStrs(pre.WillDelete)
	assert.Contains(t, del, "managed volume: data")
	assert.Contains(t, del, "managed data dir")
	// external 卷永不删 → willKeep。
	keep := joinStrs(pre.WillKeep)
	assert.Contains(t, keep, "external volume")
}

func TestController_StorageInventoryNotFound(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	ctrl, _, _, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	_, err := ctrl.StorageInventory(context.Background(), "nope")
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, ErrKindNotFound, ae.Kind)
}

func joinStrs(s []string) string {
	out := ""
	for _, x := range s {
		out += x + "\n"
	}
	return out
}
