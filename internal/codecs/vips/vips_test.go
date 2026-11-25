package vips

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/format"
)

var fixtures = filepath.Join("..", "..", "..", "testdata", "images")

func fixture(name string) string { return filepath.Join(fixtures, name) }

func newCodec(t *testing.T) *Codec {
	t.Helper()
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

var defaultOpts = codecs.Options{Quality: 90}

// convert runs a conversion into a temp dir, checks the source is untouched
// and the output really is dstFormat, and returns the output path.
func convert(t *testing.T, c *Codec, src string, srcFmt, dstFmt format.Format, opts codecs.Options) string {
	t.Helper()
	before, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "out"+dstFmt.Extension())
	if err := c.Convert(context.Background(), src, srcFmt, dst, dstFmt, opts); err != nil {
		t.Fatalf("Convert(%s -> %s): %v", filepath.Base(src), dstFmt, err)
	}
	after, err := os.ReadFile(src)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("source %s was modified", src)
	}
	got, err := format.DetectFile(dst)
	if err != nil || got != dstFmt {
		t.Fatalf("output format = %q, %v; want %q", got, err, dstFmt)
	}
	return dst
}

func decode(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return img
}

func luma(img image.Image, x, y int) int {
	r, g, b, _ := img.At(x, y).RGBA()
	return int((r + g + b) / 3 >> 8)
}

func mustProbe(t *testing.T, path string) info {
	t.Helper()
	i, err := probe(path)
	if err != nil {
		t.Fatalf("probe %s: %v", path, err)
	}
	return i
}

func TestCapabilities(t *testing.T) {
	c := newCodec(t)
	t.Log(c.Name())
	caps := c.Capabilities()
	for _, f := range format.All {
		if !codecs.CanRead(caps, f) {
			t.Errorf("cannot read %s", f)
		}
	}
	for _, f := range []format.Format{format.JPEG, format.PNG, format.WebP} {
		if !codecs.CanWrite(caps, f) {
			t.Errorf("cannot write %s", f)
		}
	}
	if codecs.CanWrite(caps, format.HEIC) {
		t.Error("HEIC write should not be advertised")
	}
}

func TestConversionMatrix(t *testing.T) {
	c := newCodec(t)
	sources := map[format.Format]string{
		format.HEIC: "heic/gradient.heic",
		format.JPEG: "jpeg/gradient.jpg",
		format.PNG:  "png/alpha.png",
		format.WebP: "webp/gradient.webp",
	}
	for srcFmt, name := range sources {
		for _, dstFmt := range []format.Format{format.JPEG, format.PNG, format.WebP} {
			t.Run(string(srcFmt)+"->"+string(dstFmt), func(t *testing.T) {
				out := convert(t, c, fixture(name), srcFmt, dstFmt, defaultOpts)
				if i := mustProbe(t, out); i.Width != 64 || i.Height != 32 {
					t.Errorf("dimensions = %dx%d; want 64x32", i.Width, i.Height)
				}
			})
		}
	}
}

// assertPortraitGradient checks pixels were rotated 90° clockwise from the
// landscape left-to-right fixture gradient: dark at the top, bright at the bottom.
func assertPortraitGradient(t *testing.T, path string) {
	t.Helper()
	img := decode(t, path)
	if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 64 {
		t.Fatalf("decoded size = %dx%d; want 32x64", b.Dx(), b.Dy())
	}
	top, bottom := luma(img, 16, 2), luma(img, 16, 61)
	if top > 40 || bottom < 200 {
		t.Errorf("gradient top=%d bottom=%d; want dark top and bright bottom", top, bottom)
	}
}

func TestHEIFTransformAppliedOnce(t *testing.T) {
	c := newCodec(t)
	for _, dst := range []format.Format{format.JPEG, format.PNG} {
		out := convert(t, c, fixture("heic/irot90.heic"), format.HEIC, dst, defaultOpts)
		assertPortraitGradient(t, out)
		if o := mustProbe(t, out).Orientation; o != 1 {
			t.Errorf("%s orientation = %d; want 1", dst, o)
		}
	}
}

func TestEXIFOrientationNormalized(t *testing.T) {
	c := newCodec(t)
	out := convert(t, c, fixture("jpeg/exif-orientation-6.jpg"), format.JPEG, format.PNG, defaultOpts)
	assertPortraitGradient(t, out)

	out = convert(t, c, fixture("jpeg/exif-orientation-6.jpg"), format.JPEG, format.JPEG, defaultOpts)
	i := mustProbe(t, out)
	if !i.HasEXIF {
		t.Error("EXIF was not preserved")
	}
	if i.Orientation != 1 {
		t.Errorf("preserved orientation = %d; want 1", i.Orientation)
	}
	assertPortraitGradient(t, out)
}

func TestAlphaFlattenedToWhiteForJPEG(t *testing.T) {
	c := newCodec(t)
	for _, tc := range []struct {
		src    string
		srcFmt format.Format
	}{
		{"png/alpha.png", format.PNG},
		{"heic/alpha.heic", format.HEIC},
	} {
		out := convert(t, c, fixture(tc.src), tc.srcFmt, format.JPEG, defaultOpts)
		img := decode(t, out)
		if l := luma(img, 4, 16); l < 245 {
			t.Errorf("%s: transparent area luma = %d; want white", tc.src, l)
		}
	}
}

func TestAlphaKeptForPNG(t *testing.T) {
	c := newCodec(t)
	out := convert(t, c, fixture("heic/alpha.heic"), format.HEIC, format.PNG, defaultOpts)
	if !mustProbe(t, out).HasAlpha {
		t.Error("PNG output lost alpha")
	}
}

func TestICCProfile(t *testing.T) {
	c := newCodec(t)
	src := fixture("heic/display-p3.heic")
	if !mustProbe(t, src).HasICC {
		t.Fatal("fixture has no ICC profile")
	}

	kept := convert(t, c, src, format.HEIC, format.JPEG, defaultOpts)
	if !mustProbe(t, kept).HasICC {
		t.Error("ICC profile not preserved")
	}

	stripped := convert(t, c, src, format.HEIC, format.JPEG,
		codecs.Options{Quality: 90, StripMetadata: true})
	i := mustProbe(t, stripped)
	if i.HasICC || i.HasEXIF {
		t.Errorf("metadata not stripped: icc=%v exif=%v", i.HasICC, i.HasEXIF)
	}
}

func TestAuxiliaryImagesIgnored(t *testing.T) {
	c := newCodec(t)
	out := convert(t, c, fixture("heic/with-thumbnail.heic"), format.HEIC, format.JPEG, defaultOpts)
	if i := mustProbe(t, out); i.Width != 64 || i.Height != 32 {
		t.Errorf("dimensions = %dx%d; want primary image 64x32", i.Width, i.Height)
	}
}

func TestErrors(t *testing.T) {
	c := newCodec(t)
	tests := []struct {
		name   string
		src    string
		srcFmt format.Format
		dstFmt format.Format
		opts   codecs.Options
		want   error
	}{
		{"multi-image", "heic/multi-image.heic", format.HEIC, format.JPEG, defaultOpts, codecs.ErrMultipleImages},
		{"empty", "invalid/empty.heic", format.HEIC, format.JPEG, defaultOpts, codecs.ErrDecode},
		{"truncated", "invalid/truncated.heic", format.HEIC, format.JPEG, defaultOpts, codecs.ErrDecode},
		{"wrong content", "invalid/text.jpg", format.JPEG, format.PNG, defaultOpts, codecs.ErrDecode},
		{"missing", "heic/does-not-exist.heic", format.HEIC, format.JPEG, defaultOpts, codecs.ErrDecode},
		{"too large", "heic/gradient.heic", format.HEIC, format.JPEG, codecs.Options{Quality: 90, MaxPixels: 100}, codecs.ErrTooLarge},
		{"heic output", "jpeg/gradient.jpg", format.JPEG, format.HEIC, defaultOpts, codecs.ErrUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := filepath.Join(t.TempDir(), "out"+tt.dstFmt.Extension())
			err := c.Convert(context.Background(), fixture(tt.src), tt.srcFmt, dst, tt.dstFmt, tt.opts)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v; want %v", err, tt.want)
			}
			t.Log(err)
		})
	}
}

func TestInvalidQuality(t *testing.T) {
	c := newCodec(t)
	dst := filepath.Join(t.TempDir(), "out.jpg")
	for _, q := range []int{0, 101} {
		err := c.Convert(context.Background(), fixture("heic/gradient.heic"), format.HEIC, dst, format.JPEG, codecs.Options{Quality: q})
		if err == nil {
			t.Errorf("quality %d accepted", q)
		}
	}
}

func TestCancelledContext(t *testing.T) {
	c := newCodec(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dst := filepath.Join(t.TempDir(), "out.jpg")
	err := c.Convert(ctx, fixture("heic/gradient.heic"), format.HEIC, dst, format.JPEG, defaultOpts)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v; want context.Canceled", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("output written despite cancellation")
	}
}

func TestConcurrentConversions(t *testing.T) {
	c := newCodec(t)
	dir := t.TempDir()
	errs := make(chan error, 16)
	for i := range 16 {
		go func() {
			dst := filepath.Join(dir, string(rune('a'+i))+".jpg")
			errs <- c.Convert(context.Background(), fixture("heic/gradient.heic"), format.HEIC, dst, format.JPEG, defaultOpts)
		}()
	}
	for range 16 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}
