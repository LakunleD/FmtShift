//go:build unix

package filesystem

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// withUmask sets the process umask for the duration of the test. The umask is
// process-wide, so these tests must not run in parallel.
func withUmask(t *testing.T, mask int) {
	t.Helper()
	old := syscall.Umask(mask)
	t.Cleanup(func() { syscall.Umask(old) })
}

func TestWriteNewRespectsUmask(t *testing.T) {
	for _, tc := range []struct {
		umask int
		want  fs.FileMode
	}{
		{0o077, 0o600},
		{0o022, 0o644},
		{0o027, 0o640},
	} {
		withUmask(t, tc.umask)
		dst := filepath.Join(t.TempDir(), "out.jpg")
		// Write the way libvips does: truncate the existing file and write.
		err := WriteNew(dst, func(tmp string) error {
			return os.WriteFile(tmp, []byte("data"), 0o666)
		})
		if err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(dst)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != tc.want {
			t.Errorf("umask %03o: mode = %v; want %v", tc.umask, got, tc.want)
		}
	}
}
