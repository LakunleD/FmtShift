// Package integration runs the conversion engine with the real libvips codec.
package integration

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/codecs/vips"
	"github.com/lakunled/fmtshift/internal/converter"
	"github.com/lakunled/fmtshift/internal/format"
)

var fixtures = filepath.Join("..", "..", "testdata", "images")

func newConverter(t *testing.T) *converter.Converter {
	t.Helper()
	codec, err := vips.New()
	if err != nil {
		t.Fatal(err)
	}
	return converter.New(codec)
}

// copyFixture copies a fixture into dir under name and returns its path and
// content.
func copyFixture(t *testing.T, fixture, dir, name string) (string, []byte) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtures, fixture))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path, b
}

func TestHEICToEveryFormat(t *testing.T) {
	cv := newConverter(t)
	dir := t.TempDir()
	src, orig := copyFixture(t, "heic/irot90.heic", dir, "IMG 0001.HEIC")

	for _, f := range []format.Format{format.JPEG, format.PNG, format.WebP} {
		res, err := cv.Convert(context.Background(), converter.Request{SourcePath: src, Format: f})
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if got, err := format.DetectFile(res.OutputPath); err != nil || got != f {
			t.Errorf("%s: output detected as %q, %v", f, got, err)
		}
		if res.OutputSize == 0 || res.Duration <= 0 {
			t.Errorf("%s: result = %+v", f, res)
		}
	}
	if b, _ := os.ReadFile(src); !bytes.Equal(b, orig) {
		t.Fatal("source modified")
	}
}

// A HEIC saved as photo.jpg is a common real-world case. Converting it to
// JPEG targets its own name and must be refused, even with Overwrite.
func TestMisnamedHEICIsNotOverwritten(t *testing.T) {
	cv := newConverter(t)
	dir := t.TempDir()
	src, orig := copyFixture(t, "heic/gradient.heic", dir, "photo.jpg")

	_, err := cv.Convert(context.Background(), converter.Request{SourcePath: src, Format: format.JPEG, Overwrite: true})
	if !errors.Is(err, converter.ErrSourceIsOutput) {
		t.Fatalf("err = %v; want ErrSourceIsOutput", err)
	}
	if b, _ := os.ReadFile(src); !bytes.Equal(b, orig) {
		t.Fatal("source modified")
	}

	// Into another directory it converts normally.
	out := filepath.Join(dir, "out")
	res, err := cv.Convert(context.Background(), converter.Request{SourcePath: src, Format: format.JPEG, OutputDir: out, CreateOutputDir: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := format.DetectFile(res.OutputPath); got != format.JPEG {
		t.Errorf("output detected as %q", got)
	}
}

func TestCodecRejectionsSurfaceAsUnsupportedInput(t *testing.T) {
	cv := newConverter(t)
	dir := t.TempDir()
	for fixture, cause := range map[string]error{
		"heic/hdr-pq.heic":      codecs.ErrHDR,
		"heic/multi-image.heic": codecs.ErrMultipleImages,
	} {
		src, _ := copyFixture(t, fixture, dir, filepath.Base(fixture))
		res, err := cv.Convert(context.Background(), converter.Request{SourcePath: src, Format: format.JPEG})
		if !errors.Is(err, converter.ErrUnsupportedInput) || !errors.Is(err, cause) {
			t.Errorf("%s: err = %v; want ErrUnsupportedInput wrapping %v", fixture, err, cause)
		}
		if _, statErr := os.Stat(res.OutputPath); !os.IsNotExist(statErr) {
			t.Errorf("%s: output written for rejected input", fixture)
		}
	}
}

func TestCorruptInput(t *testing.T) {
	cv := newConverter(t)
	dir := t.TempDir()
	src, _ := copyFixture(t, "invalid/truncated.heic", dir, "broken.heic")
	_, err := cv.Convert(context.Background(), converter.Request{SourcePath: src, Format: format.PNG})
	if !errors.Is(err, converter.ErrCorrupt) {
		t.Fatalf("err = %v; want ErrCorrupt", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("directory contains %d entries; want only the source", len(entries))
	}
}
