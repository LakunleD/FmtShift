//go:build unix

package vips

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lakunled/fmtshift/internal/format"
)

// filesystem.WriteNew relies on the codec writing into the existing
// temporary file, keeping its umask-derived permissions. If libvips ever
// unlinks and recreates the file, this test fails.
func TestConvertKeepsDestinationMode(t *testing.T) {
	c := newCodec(t)
	for _, dstFmt := range []format.Format{format.JPEG, format.PNG, format.WebP} {
		dst := filepath.Join(t.TempDir(), "out"+dstFmt.Extension())
		if err := os.WriteFile(dst, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dst, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := c.Convert(context.Background(), fixture("heic/gradient.heic"), format.HEIC, dst, dstFmt, defaultOpts); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(dst)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode = %v; want 0600", dstFmt, fi.Mode().Perm())
		}
		if fi.Size() == 0 {
			t.Errorf("%s: output is empty", dstFmt)
		}
	}
}
