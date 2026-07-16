package vms

import "testing"

func TestParseFilesystems(t *testing.T) {
	xml := `
<domain type='kvm'>
  <devices>
    <filesystem type='mount' accessmode='passthrough'>
      <driver type='virtiofs'/>
      <source dir='/data/_system/vm-shared/gpu1080-16c32g-data3'/>
      <target dir='data3'/>
    </filesystem>
  </devices>
</domain>`

	filesystems := parseFilesystems(xml)
	if len(filesystems) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(filesystems))
	}
	fs := filesystems[0]
	if fs.Source != "/data/_system/vm-shared/gpu1080-16c32g-data3" {
		t.Fatalf("unexpected source: %q", fs.Source)
	}
	if fs.Target != "data3" {
		t.Fatalf("unexpected target: %q", fs.Target)
	}
	if fs.Type != "mount" || fs.AccessMode != "passthrough" || fs.Driver != "virtiofs" {
		t.Fatalf("unexpected filesystem metadata: %#v", fs)
	}
}

func TestParseGuestOutputMounts(t *testing.T) {
	var snap GuestSnapshot
	parseGuestOutput(&snap, `0.10 0.20 0.30 1/2 3
               total        used        free      shared  buff/cache   available
Mem:      1000000000   400000000   100000000    10000000   500000000   600000000
Swap:              0           0           0
some avg10=0.00 avg60=0.00 avg300=0.00 total=0
full avg10=0.00 avg60=0.00 avg300=0.00 total=0
DEVBOX_MOUNTS
data3 /mnt/data3 virtiofs
`)

	if len(snap.Mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(snap.Mounts))
	}
	mount := snap.Mounts[0]
	if mount.Source != "data3" || mount.Target != "/mnt/data3" || mount.FSType != "virtiofs" {
		t.Fatalf("unexpected mount: %#v", mount)
	}
}
