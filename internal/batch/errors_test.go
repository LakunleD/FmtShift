//go:build unix

package batch

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lakunled/fmtshift/internal/converter"
	"github.com/lakunled/fmtshift/internal/format"
)

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permission bits are not enforced for root")
	}
}

// Filesystem errors found during discovery carry the converter's kinds, so
// they are reported as "permission denied" or "not found", not generically.
func TestDiscoveryErrorsAreClassified(t *testing.T) {
	t.Run("unreadable file", func(t *testing.T) {
		skipIfRoot(t)
		root := t.TempDir()
		tree(t, root, map[string]format.Format{"locked.heic": format.HEIC})
		locked := filepath.Join(root, "locked.heic")
		os.Chmod(locked, 0)
		t.Cleanup(func() { os.Chmod(locked, 0o644) })

		it := byName(t, root, mustDiscover(t, []string{root}, Options{Template: jpegTemplate("")}))["locked.heic"]
		if it.Outcome != Failed || !errors.Is(it.Err, converter.ErrPermission) {
			t.Errorf("outcome %v, err %v; want Failed with ErrPermission", it.Outcome, it.Err)
		}
	})

	t.Run("file vanishes during the scan", func(t *testing.T) {
		root := t.TempDir()
		tree(t, root, map[string]format.Format{"gone.heic": format.HEIC})
		testHookVisit = func() { os.Remove(filepath.Join(root, "gone.heic")) }
		t.Cleanup(func() { testHookVisit = nil })

		it := byName(t, root, mustDiscover(t, []string{root}, Options{Template: jpegTemplate("")}))["gone.heic"]
		if it.Outcome != Failed || !errors.Is(it.Err, converter.ErrNotFound) {
			t.Errorf("outcome %v, err %v; want Failed with ErrNotFound", it.Outcome, it.Err)
		}
	})

	t.Run("unreadable subfolder", func(t *testing.T) {
		skipIfRoot(t)
		root := t.TempDir()
		tree(t, root, map[string]format.Format{"a.heic": format.HEIC, "private/b.heic": format.HEIC})
		private := filepath.Join(root, "private")
		os.Chmod(private, 0)
		t.Cleanup(func() { os.Chmod(private, 0o755) })

		got := byName(t, root, mustDiscover(t, []string{root}, Options{Recursive: true, Template: jpegTemplate("")}))
		if it := got["private"]; it == nil || it.Outcome != Failed || !errors.Is(it.Err, converter.ErrPermission) {
			t.Errorf("private: %+v; want Failed with ErrPermission", it)
		}
		if got["a.heic"].Outcome != Pending {
			t.Error("an unreadable subfolder stopped the rest of the scan")
		}
	})
}
