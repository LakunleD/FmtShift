package vips

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lakunled/fmtshift/internal/format"
)

// writeWarningJPEG writes a valid JPEG with an APP0 JFIF segment carrying an
// unknown version (19.55). libjpeg warns about it but decodes the image, so
// a successful conversion leaves a warning in the libvips error buffer.
func writeWarningJPEG(t *testing.T, dir string) string {
	t.Helper()
	orig, err := os.ReadFile(fixture("jpeg/gradient.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	app0 := []byte{
		0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00,
		0x13, 0x37, // version 19.55
		0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
	}
	b := append(append([]byte{}, orig[:2]...), app0...) // SOI + APP0
	b = append(b, orig[2:]...)
	path := filepath.Join(dir, "warns.jpg")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Messages left in the process-wide buffer by earlier work (here, a warning
// from a successful conversion) must not leak into a later probe error.
func TestProbeErrorNotContaminatedByEarlierWarnings(t *testing.T) {
	c := newCodec(t)
	dir := t.TempDir()
	src := writeWarningJPEG(t, dir)
	if err := convertPath(context.Background(), c, src, format.JPEG, filepath.Join(dir, "out.png"), format.PNG, defaultOpts); err != nil {
		t.Fatalf("warning-only JPEG should convert: %v", err)
	}

	_, err := probe(filepath.Join(dir, "missing.png"))
	if err == nil {
		t.Fatal("probe of missing file succeeded")
	}
	if strings.Contains(err.Error(), "JFIF") {
		t.Fatalf("probe error contains a stale message from an earlier conversion:\n%v", err)
	}
}

// The same applies to a conversion's own diagnostic retry.
func TestConvertErrorNotContaminatedByEarlierWarnings(t *testing.T) {
	c := newCodec(t)
	dir := t.TempDir()
	src := writeWarningJPEG(t, dir)
	if err := convertPath(context.Background(), c, src, format.JPEG, filepath.Join(dir, "out.png"), format.PNG, defaultOpts); err != nil {
		t.Fatal(err)
	}
	err := convertPath(context.Background(), c, fixture("invalid/truncated.heic"), format.HEIC, filepath.Join(dir, "bad.png"), format.PNG, defaultOpts)
	if err == nil {
		t.Fatal("truncated HEIC converted")
	}
	if strings.Contains(err.Error(), "JFIF") {
		t.Fatalf("conversion error contains a stale message:\n%v", err)
	}
}
