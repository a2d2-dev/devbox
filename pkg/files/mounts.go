package files

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var networkFilesystems = map[string]bool{
	"9p": true, "afs": true, "ceph": true, "cifs": true, "davfs": true,
	"fuse.sshfs": true, "glusterfs": true, "nfs": true, "nfs4": true,
	"smb3": true, "sshfs": true,
}

func discoverMountSources(mountsFile, primaryRoot, appsRoot string) []Source {
	f, err := os.Open(mountsFile)
	if err != nil {
		return nil
	}
	defer f.Close()

	seen := map[string]bool{}
	var result []Source
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		mountpoint := decodeMountField(fields[1])
		fsType := fields[2]
		kind := ""
		namePrefix := ""
		if networkFilesystems[fsType] || strings.HasPrefix(fsType, "fuse.sshfs") {
			kind, namePrefix = "network", "远程"
		} else if isExternalMount(mountpoint) {
			kind, namePrefix = "external", "外接"
		}
		if kind == "" || seen[mountpoint] || filepath.Clean(mountpoint) == filepath.Clean(primaryRoot) || filepath.Clean(mountpoint) == filepath.Clean(appsRoot) {
			continue
		}
		info, statErr := os.Stat(mountpoint)
		if statErr != nil || !info.IsDir() {
			continue
		}
		seen[mountpoint] = true
		sum := sha256.Sum256([]byte(kind + "\x00" + mountpoint))
		name := filepath.Base(mountpoint)
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = mountpoint
		}
		result = append(result, Source{
			ID: fmt.Sprintf("%s-%x", kind, sum[:6]), Name: namePrefix + " · " + name,
			Kind: kind, Available: true, Root: mountpoint, Capabilities: readOnlyCapabilities(),
		})
	}
	return result
}

func (b *Browser) isForeignMount(source Source, candidate string) bool {
	return withinMountRoots(b.foreignMountRoots(source), candidate)
}

func (b *Browser) containsForeignMount(source Source, candidate string) bool {
	for _, root := range b.foreignMountRoots(source) {
		if pathWithin(candidate, root) {
			return true
		}
	}
	return false
}

func (b *Browser) foreignMountRoots(source Source) []string {
	var roots []string
	for _, mount := range discoverMountSources(b.mountsFile, b.rootDir, b.appsDir) {
		if filepath.Clean(mount.Root) != filepath.Clean(source.Root) {
			roots = append(roots, mount.Root)
		}
	}
	return roots
}

func withinMountRoots(roots []string, candidate string) bool {
	for _, root := range roots {
		if pathWithin(root, candidate) {
			return true
		}
	}
	return false
}

func decodeMountField(value string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return replacer.Replace(value)
}

func isExternalMount(path string) bool {
	for _, prefix := range []string{"/media", "/mnt", "/run/media", "/Volumes"} {
		if path == prefix || strings.HasPrefix(path, prefix+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func pathWithin(root, candidate string) bool {
	root = filepath.Clean(root)
	candidate = filepath.Clean(candidate)
	return candidate == root || strings.HasPrefix(candidate, root+string(filepath.Separator))
}
