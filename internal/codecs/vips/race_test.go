package vips

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/format"
)

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// If the source path is replaced after colour inspection, decoding must
// still read the original file. The replacement has different dimensions
// (portrait irot90 vs landscape gradient), so reading it would show.
func TestSourceReplacedDuringConversion(t *testing.T) {
	c := newCodec(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "photo.heic")
	swap := filepath.Join(dir, "swap.heic")
	copyFile(t, fixture("heic/gradient.heic"), src)
	copyFile(t, fixture("heic/irot90.heic"), swap)

	swapped := false
	testHookBeforeDecode = func() {
		if !swapped {
			swapped = true
			if err := os.Rename(swap, src); err != nil {
				t.Error(err)
			}
		}
	}
	t.Cleanup(func() { testHookBeforeDecode = nil })

	dst := filepath.Join(dir, "out.png")
	if err := c.Convert(context.Background(), src, format.HEIC, dst, format.PNG, defaultOpts); err != nil {
		t.Fatal(err)
	}
	if !swapped {
		t.Fatal("hook did not run")
	}
	if i := mustProbe(t, dst); i.Width != 64 || i.Height != 32 {
		t.Fatalf("output is %dx%d; decoded the replacement file instead of the inspected one", i.Width, i.Height)
	}
}

// writeCorrupt creates a file with a valid signature for f followed by junk,
// so it passes format detection and fails inside libvips/libheif.
func writeCorrupt(t *testing.T, dir string, f format.Format, i int) string {
	t.Helper()
	var head []byte
	switch f {
	case format.JPEG:
		head = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0}
	case format.PNG:
		head = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")
	case format.WebP:
		head = []byte("RIFF\x40\x00\x00\x00WEBPVP8 ")
	case format.HEIC:
		b, err := os.ReadFile(fixture("invalid/truncated.heic"))
		if err != nil {
			t.Fatal(err)
		}
		head = b
	}
	junk := []byte(strings.Repeat("\x13\x37", 64))
	path := filepath.Join(dir, fmt.Sprintf("bad-%s-%d%s", f, i, f.Extension()))
	if err := os.WriteFile(path, append(head, junk...), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// errorSignature returns text that only the given format's decoder emits.
var errorSignature = map[format.Format]string{}

func TestConcurrentErrorsAreAttributed(t *testing.T) {
	c := newCodec(t)
	dir := t.TempDir()
	formats := []format.Format{format.JPEG, format.PNG, format.WebP, format.HEIC}

	// Learn each decoder's message when run alone.
	solo := map[format.Format]string{}
	for _, f := range formats {
		src := writeCorrupt(t, dir, f, 0)
		err := c.Convert(context.Background(), src, f, filepath.Join(dir, "solo.png"), format.PNG, defaultOpts)
		if !errors.Is(err, codecs.ErrDecode) && !errors.Is(err, codecs.ErrConversion) {
			t.Fatalf("%s: err = %v; want decode or conversion error", f, err)
		}
		solo[f] = strings.TrimPrefix(err.Error(), "")
		t.Logf("%s: %v", f, err)
	}

	// Run many failing and succeeding conversions at once. Each error must
	// name its own file and carry exactly the message its decoder gives
	// when run alone, with nothing from other conversions mixed in.
	const rounds = 8
	var wg sync.WaitGroup
	for i := 1; i <= rounds; i++ {
		for _, f := range formats {
			src := writeCorrupt(t, dir, f, i)
			wg.Add(2)
			go func() {
				defer wg.Done()
				dst := filepath.Join(dir, filepath.Base(src)+".png")
				err := c.Convert(context.Background(), src, f, dst, format.PNG, defaultOpts)
				if err == nil {
					t.Errorf("%s: unexpected success", src)
					return
				}
				want := strings.ReplaceAll(solo[f], fmt.Sprintf("bad-%s-0", f), fmt.Sprintf("bad-%s-%d", f, i))
				if err.Error() != want {
					t.Errorf("concurrent error for %s:\n got: %v\nwant: %s", filepath.Base(src), err, want)
				}
			}()
			go func() {
				defer wg.Done()
				dst := filepath.Join(dir, fmt.Sprintf("ok-%s-%d.jpg", f, i))
				if err := c.Convert(context.Background(), fixture("heic/gradient.heic"), format.HEIC, dst, format.JPEG, defaultOpts); err != nil {
					t.Errorf("successful conversion failed: %v", err)
				}
			}()
		}
	}
	wg.Wait()
}
