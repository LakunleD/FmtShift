//go:build unix

package batch

import (
	"context"
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/lakunled/fmtshift/internal/converter"
	"github.com/lakunled/fmtshift/internal/format"
)

// within fails the test if fn does not return in time: opening a FIFO for
// reading blocks forever without a writer, so a hang means a blocking open.
func within(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("blocked: a FIFO was opened with a blocking open")
	}
}

func TestFIFOInputsDoNotBlock(t *testing.T) {
	for name, opt := range map[string]Options{
		"plain":       {Template: jpegTemplate("")},
		"with --from": {From: []format.Format{format.HEIC}, Template: jpegTemplate("")},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			tree(t, root, map[string]format.Format{"a.heic": format.HEIC, "dir/b.heic": format.HEIC})
			fifo := filepath.Join(root, "pipe.heic")
			if err := syscall.Mkfifo(fifo, 0o644); err != nil {
				t.Skip(err)
			}
			if err := syscall.Mkfifo(filepath.Join(root, "dir", "pipe2.heic"), 0o644); err != nil {
				t.Skip(err)
			}
			var items []*Item
			within(t, 5*time.Second, func() {
				// Explicit FIFO next to a regular file (duplicate preflight
				// opens both) and a FIFO inside a scanned folder.
				items = mustDiscover(t, []string{filepath.Join(root, "a.heic"), fifo, filepath.Join(root, "dir")}, opt)
				Run(context.Background(), converter.New(&fakeCodec{}), items, 2, func(*Item) {})
			})
			got := byName(t, root, items)
			if it := got["pipe.heic"]; it.Outcome != Failed || !errors.Is(it.Err, converter.ErrUnsupportedInput) {
				t.Errorf("explicit FIFO: outcome %v, err %v; want failed as unsupported input", it.Outcome, it.Err)
			}
			if _, found := got["dir/pipe2.heic"]; found {
				t.Error("FIFO inside a folder was treated as an input")
			}
			if got["a.heic"].Outcome != Converted || got["dir/b.heic"].Outcome != Converted {
				t.Errorf("regular files: %v %v", got["a.heic"].Outcome, got["dir/b.heic"].Outcome)
			}
		})
	}
}
