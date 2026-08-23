package hardware

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseStorageJSONClassifiesAndFlattens(t *testing.T) {
	raw := `{"blockdevices":[{"name":"sda","path":"/dev/sda","model":" SATA SSD ","size":1000,"rota":false,"tran":"sata","type":"disk","children":[{"name":"sda1","path":"/dev/sda1","size":900,"fstype":"ext4","mountpoints":["/data"]}]},{"name":"nvme0n1","path":"/dev/nvme0n1","size":2000,"rota":false,"tran":"nvme","type":"disk"},{"name":"sdb","path":"/dev/sdb","size":3000,"rota":true,"tran":"usb","type":"disk"}]}`
	disks, err := parseStorageJSON(raw)
	require.NoError(t, err)
	require.Len(t, disks, 3)
	require.Equal(t, "SSD", disks[0].Medium)
	require.Equal(t, "internal", disks[0].Category)
	require.Equal(t, "数据", disks[0].Partitions[0].Purpose)
	require.Equal(t, "NVMe", disks[1].Medium)
	require.Equal(t, "NVME", disks[1].Interface)
	require.Equal(t, "HDD", disks[2].Medium)
	require.Equal(t, "external", disks[2].Category)
}

func TestParseSMARTJSON(t *testing.T) {
	h, ok := parseSMARTJSON([]byte(`{"smart_status":{"passed":true}}`))
	require.True(t, ok)
	require.Equal(t, "healthy", h.Status)
	h, ok = parseSMARTJSON([]byte(`{"smart_status":{"passed":false}}`))
	require.True(t, ok)
	require.Equal(t, "failing", h.Status)
	_, ok = parseSMARTJSON([]byte(`{}`))
	require.False(t, ok)
}

func TestParseMountsJSONExcludesLoopDevices(t *testing.T) {
	raw := `{"filesystems":[{"source":"/dev/sda1","target":"/data","fstype":"ext4","size":100,"used":25,"avail":75,"use%":"25%","children":[{"source":"/dev/loop0","target":"/snap/x","fstype":"squashfs","size":1,"used":1,"avail":0,"use%":"100%"}]}]}`
	mounts := parseMountsJSON(raw)
	require.Len(t, mounts, 1)
	require.Equal(t, "/data", mounts[0].Path)
	require.Equal(t, 25.0, mounts[0].UsagePct)
}
