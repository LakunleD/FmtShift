package converter

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/format"
)

// copyingCodec writes the bytes it reads from the source handle to the
// output, so tests can see exactly which file was converted. before runs
// first, to simulate the source path changing after the engine's checks.
func copyingCodec(before func()) *fakeCodec {
	return &fakeCodec{convert: func(_ context.Context, src *os.File, tmp string, _ codecs.Options) error {
		if before != nil {
			before()
		}
		b, err := io.ReadAll(io.NewSectionReader(src, 0, 1<<30))
		if err != nil {
			return err
		}
		return os.WriteFile(tmp, b, 0o644)
	}}
}

// If the source path is replaced after the engine's checks, the conversion
// must still use the file that was checked, and the result must describe it.
func TestSourceReplacedAfterChecks(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "photo.heic")
	orig := writeSource(t, src, format.HEIC)
	other := filepath.Join(dir, "other.heic")
	impostor := append(writeSource(t, other, format.HEIC), "-impostor-with-more-bytes"...)
	os.WriteFile(other, impostor, 0o644)

	fc := copyingCodec(func() {
		if err := os.Rename(other, src); err != nil {
			t.Error(err)
		}
	})
	res, err := New(fc).Convert(context.Background(), Request{SourcePath: src, Format: format.JPEG})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(res.OutputPath)
	if !bytes.Equal(got, orig) {
		t.Fatalf("output came from the replacement file, not the checked source")
	}
	if res.SourceSize != int64(len(orig)) {
		t.Errorf("SourceSize = %d; want %d (the checked file)", res.SourceSize, len(orig))
	}
}

// A source symlink retargeted at the destination after the checks must not
// turn an overwrite into "convert the destination onto itself". The checked
// file is converted and the destination's old content is not read.
func TestSourceSymlinkRetargetedToDestination(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.heic")
	orig := writeSource(t, real, format.HEIC)
	link := filepath.Join(dir, "in", "photo.heic")
	os.Mkdir(filepath.Dir(link), 0o755)
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	out := filepath.Join(dir, "out")
	os.Mkdir(out, 0o755)
	dst := filepath.Join(out, "photo.jpg")
	// Valid HEIC signature (so a retargeted read would "work") but content
	// distinct from the real source, so reading it would be detected.
	writeSource(t, dst, format.HEIC)
	os.WriteFile(dst, append(append([]byte{}, signatures[format.HEIC]...), "old output"...), 0o644)

	fc := copyingCodec(func() {
		os.Remove(link)
		if err := os.Symlink(dst, link); err != nil {
			t.Error(err)
		}
	})
	_, err := New(fc).Convert(context.Background(), Request{SourcePath: link, Format: format.JPEG, OutputDir: out, Overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, orig) {
		t.Fatalf("destination holds %q; want content of the checked source", got)
	}
	if b, _ := os.ReadFile(real); !bytes.Equal(b, orig) {
		t.Fatal("original source modified")
	}
}

func TestDurationOnError(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "photo.heic")
	writeSource(t, src, format.HEIC)

	const delay = 20 * time.Millisecond
	fc := &fakeCodec{convert: func(context.Context, *os.File, string, codecs.Options) error {
		time.Sleep(delay)
		return codecs.ErrDecode
	}}
	res, err := New(fc).Convert(context.Background(), Request{SourcePath: src, Format: format.JPEG})
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v", err)
	}
	if res.Duration < delay {
		t.Errorf("Duration = %v on failure; want ≥ %v", res.Duration, delay)
	}

	// Early failures report a duration too.
	res, err = New(&fakeCodec{}).Convert(context.Background(), Request{SourcePath: filepath.Join(dir, "missing.heic"), Format: format.JPEG})
	if !errors.Is(err, ErrNotFound) || res.Duration <= 0 {
		t.Errorf("missing source: err = %v, Duration = %v; want ErrNotFound and a positive duration", err, res.Duration)
	}
}
