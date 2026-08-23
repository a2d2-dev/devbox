package files

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBrowserRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "escape")))
	b := NewBrowser(Config{RootDir: root})

	_, err := b.List("escape")
	require.ErrorContains(t, err, "access denied")
	_, err = b.ResolveFile("escape", "secret.txt")
	require.ErrorContains(t, err, "access denied")
	_, err = b.Save("escape", "created.txt", []byte("no"))
	require.ErrorContains(t, err, "access denied")
	require.NoFileExists(t, filepath.Join(outside, "created.txt"))
}
