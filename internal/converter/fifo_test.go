//go:build unix

package converter

import (
	"context"
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/lakunled/fmtshift/internal/format"
)

// A named pipe at the source path must be rejected promptly. Opening a FIFO
// for reading normally blocks until a writer appears, which would hang the
// conversion (or a whole batch) forever.
func TestFIFOSourceDoesNotHang(t *testing.T) {
	src := filepath.Join(t.TempDir(), "photo.heic")
	if err := syscall.Mkfifo(src, 0o644); err != nil {
		t.Skip(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := New(&fakeCodec{}).Convert(context.Background(), Request{SourcePath: src, Format: format.JPEG})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrUnsupportedInput) {
			t.Fatalf("err = %v; want ErrUnsupportedInput", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("conversion blocked on a FIFO source")
	}
}
