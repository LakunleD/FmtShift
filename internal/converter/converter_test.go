package converter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/filesystem"
	"github.com/lakunled/fmtshift/internal/format"
)

// fakeCodec writes a marker instead of decoding, so engine behaviour can be
// tested without native libraries.
type fakeCodec struct {
	calls atomic.Int32
	// convert overrides the default behaviour when set.
	convert func(ctx context.Context, src *os.File, dst string, opts codecs.Options) error
	last    codecs.Options
	mu      sync.Mutex
}

func (f *fakeCodec) Name() string { return "fake" }

func (f *fakeCodec) Capabilities() []codecs.Capability {
	return []codecs.Capability{
		{Format: format.HEIC, Read: true},
		{Format: format.JPEG, Read: true, Write: true},
		{Format: format.PNG, Read: true, Write: true},
		{Format: format.WebP, Read: true, Write: true},
	}
}

func (f *fakeCodec) Convert(ctx context.Context, src *os.File, _ format.Format, dst string, dstFmt format.Format, opts codecs.Options) error {
	f.calls.Add(1)
	f.mu.Lock()
	f.last = opts
	f.mu.Unlock()
	if f.convert != nil {
		return f.convert(ctx, src, dst, opts)
	}
	return os.WriteFile(dst, []byte("converted:"+string(dstFmt)), 0o666)
}

// Minimal content signatures; the fake codec never decodes them.
var signatures = map[format.Format][]byte{
	format.HEIC: []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic"),
	format.JPEG: {0xFF, 0xD8, 0xFF, 0xE0},
	format.PNG:  []byte("\x89PNG\r\n\x1a\n"),
	format.WebP: []byte("RIFF\x00\x00\x00\x00WEBPVP8 "),
}

// writeSource creates a file whose content is format f, regardless of name.
func writeSource(t *testing.T, path string, f format.Format) []byte {
	t.Helper()
	content := append(append([]byte{}, signatures[f]...), "pixels"...)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return content
}

func assertUnchanged(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("source %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("source %s was modified", path)
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".fmtshift-tmp") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestConvertDefaults(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "IMG 0001.HEIC")
	orig := writeSource(t, src, format.HEIC)
	fc := &fakeCodec{}

	res, err := New(fc).Convert(context.Background(), Request{SourcePath: src, Format: format.JPEG})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "IMG 0001.jpg")
	if res.OutputPath != want {
		t.Errorf("OutputPath = %q; want %q", res.OutputPath, want)
	}
	if b, _ := os.ReadFile(want); string(b) != "converted:jpeg" {
		t.Errorf("output content = %q", b)
	}
	if res.SourceFormat != format.HEIC || res.OutputFormat != format.JPEG {
		t.Errorf("formats = %s -> %s", res.SourceFormat, res.OutputFormat)
	}
	if res.SourceSize != int64(len(orig)) || res.OutputSize != int64(len("converted:jpeg")) {
		t.Errorf("sizes = %d -> %d", res.SourceSize, res.OutputSize)
	}
	if fc.last.Quality != DefaultQuality || fc.last.StripMetadata {
		t.Errorf("codec options = %+v; want default quality and kept metadata", fc.last)
	}
	assertUnchanged(t, src, orig)
	assertNoTempFiles(t, dir)
}

func TestOutputNames(t *testing.T) {
	tests := []struct{ src, target, want string }{
		{"photo.heic", "png", "photo.png"},
		{"photo.HEIC", "jpeg", "photo.jpg"},
		{"archive.tar.heic", "webp", "archive.tar.webp"},
		{"no-extension", "jpeg", "no-extension.jpg"},
		{".heic", "jpeg", ".heic.jpg"},
		{"café ☕ 写真.heic", "jpeg", "café ☕ 写真.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, tt.src)
			writeSource(t, src, format.HEIC)
			res, err := New(&fakeCodec{}).Convert(context.Background(), Request{SourcePath: src, Format: format.Format(tt.target)})
			if err != nil {
				t.Fatal(err)
			}
			if got := filepath.Base(res.OutputPath); got != tt.want {
				t.Errorf("output name = %q; want %q", got, tt.want)
			}
			if _, err := os.Stat(res.OutputPath); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestOutputDirectory(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "in", "photo.heic")
	writeSource(t, src, format.HEIC)
	cv := New(&fakeCodec{})
	ctx := context.Background()

	t.Run("existing", func(t *testing.T) {
		out := filepath.Join(root, "existing")
		os.Mkdir(out, 0o755)
		res, err := cv.Convert(ctx, Request{SourcePath: src, Format: format.PNG, OutputDir: out})
		if err != nil || res.OutputPath != filepath.Join(out, "photo.png") {
			t.Fatalf("got %q, %v", res.OutputPath, err)
		}
	})
	t.Run("missing without create", func(t *testing.T) {
		out := filepath.Join(root, "missing")
		_, err := cv.Convert(ctx, Request{SourcePath: src, Format: format.PNG, OutputDir: out})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v; want ErrNotFound", err)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Error("directory was created without CreateOutputDir")
		}
	})
	t.Run("missing with create", func(t *testing.T) {
		out := filepath.Join(root, "new", "nested")
		res, err := cv.Convert(ctx, Request{SourcePath: src, Format: format.PNG, OutputDir: out, CreateOutputDir: true})
		if err != nil || res.OutputPath != filepath.Join(out, "photo.png") {
			t.Fatalf("got %q, %v", res.OutputPath, err)
		}
	})
	t.Run("is a file", func(t *testing.T) {
		out := filepath.Join(root, "file")
		os.WriteFile(out, nil, 0o644)
		_, err := cv.Convert(ctx, Request{SourcePath: src, Format: format.PNG, OutputDir: out})
		if !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("err = %v; want ErrInvalidOptions", err)
		}
	})
}

func TestReadOnlyOutputDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits not enforced")
	}
	root := t.TempDir()
	src := filepath.Join(root, "photo.heic")
	writeSource(t, src, format.HEIC)
	out := filepath.Join(root, "ro")
	os.Mkdir(out, 0o555)
	t.Cleanup(func() { os.Chmod(out, 0o755) })

	_, err := New(&fakeCodec{}).Convert(context.Background(), Request{SourcePath: src, Format: format.JPEG, OutputDir: out})
	if !errors.Is(err, ErrPermission) {
		t.Fatalf("err = %v; want ErrPermission", err)
	}
}

func TestExistingOutput(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "photo.heic")
	writeSource(t, src, format.HEIC)
	dst := filepath.Join(dir, "photo.jpg")
	os.WriteFile(dst, []byte("keep me"), 0o644)

	fc := &fakeCodec{}
	_, err := New(fc).Convert(context.Background(), Request{SourcePath: src, Format: format.JPEG})
	if !errors.Is(err, ErrOutputExists) {
		t.Fatalf("err = %v; want ErrOutputExists", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "keep me" {
		t.Errorf("existing output changed to %q", b)
	}
	if fc.calls.Load() != 0 {
		t.Error("codec ran although the output exists")
	}

	res, err := New(fc).Convert(context.Background(), Request{SourcePath: src, Format: format.JPEG, Overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(res.OutputPath); string(b) != "converted:jpeg" {
		t.Errorf("overwritten output = %q", b)
	}
	assertNoTempFiles(t, dir)
}

// The destination may appear while the codec runs; without Overwrite it must
// still not be replaced.
func TestOutputAppearsDuringConversion(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "photo.heic")
	writeSource(t, src, format.HEIC)
	dst := filepath.Join(dir, "photo.jpg")
	fc := &fakeCodec{convert: func(_ context.Context, _ *os.File, tmp string, _ codecs.Options) error {
		os.WriteFile(dst, []byte("racer"), 0o644)
		return os.WriteFile(tmp, []byte("ours"), 0o644)
	}}
	_, err := New(fc).Convert(context.Background(), Request{SourcePath: src, Format: format.JPEG})
	if !errors.Is(err, ErrOutputExists) {
		t.Fatalf("err = %v; want ErrOutputExists", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "racer" {
		t.Errorf("racing file replaced with %q", b)
	}
	assertNoTempFiles(t, dir)
}

func TestSameFormatSkipped(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name   string
		f      format.Format
		target format.Format
	}{
		{"photo.jpg", format.JPEG, format.JPEG},
		{"photo.jpeg", format.JPEG, format.JPEG}, // JPG and JPEG are the same format
		{"image.png", format.PNG, format.PNG},
	} {
		src := filepath.Join(dir, tc.name)
		orig := writeSource(t, src, tc.f)
		fc := &fakeCodec{}
		_, err := New(fc).Convert(context.Background(), Request{SourcePath: src, Format: tc.target})
		if !errors.Is(err, ErrSameFormat) {
			t.Errorf("%s: err = %v; want ErrSameFormat", tc.name, err)
		}
		if fc.calls.Load() != 0 {
			t.Errorf("%s: codec ran", tc.name)
		}
		assertUnchanged(t, src, orig)
	}
}

// The output must never be the source, whatever path tricks lead there.
// Overwrite is set in every case, because that is when it would be lost.
func TestSourceNeverReplaced(t *testing.T) {
	type setup func(t *testing.T, dir string) (src string, outDir string)
	cases := map[string]setup{
		// HEIC content named photo.jpg: converting to JPEG targets photo.jpg.
		"misnamed source": func(t *testing.T, dir string) (string, string) {
			src := filepath.Join(dir, "photo.jpg")
			writeSource(t, src, format.HEIC)
			return src, ""
		},
		"symlink at destination": func(t *testing.T, dir string) (string, string) {
			src := filepath.Join(dir, "photo.heic")
			writeSource(t, src, format.HEIC)
			out := filepath.Join(dir, "out")
			os.Mkdir(out, 0o755)
			if err := os.Symlink(src, filepath.Join(out, "photo.jpg")); err != nil {
				t.Skip(err)
			}
			return src, out
		},
		"hard link at destination": func(t *testing.T, dir string) (string, string) {
			src := filepath.Join(dir, "photo.heic")
			writeSource(t, src, format.HEIC)
			out := filepath.Join(dir, "out")
			os.Mkdir(out, 0o755)
			if err := os.Link(src, filepath.Join(out, "photo.jpg")); err != nil {
				t.Skip(err)
			}
			return src, out
		},
		"output dir is a symlink to source dir": func(t *testing.T, dir string) (string, string) {
			src := filepath.Join(dir, "in", "photo.jpg")
			writeSource(t, src, format.HEIC)
			link := filepath.Join(dir, "alias")
			if err := os.Symlink(filepath.Join(dir, "in"), link); err != nil {
				t.Skip(err)
			}
			return src, link
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			src, out := setup(t, dir)
			orig, _ := os.ReadFile(src)
			fc := &fakeCodec{}
			_, err := New(fc).Convert(context.Background(), Request{SourcePath: src, Format: format.JPEG, OutputDir: out, Overwrite: true})
			if !errors.Is(err, ErrSourceIsOutput) {
				t.Fatalf("err = %v; want ErrSourceIsOutput", err)
			}
			if fc.calls.Load() != 0 {
				t.Error("codec ran")
			}
			assertUnchanged(t, src, orig)
		})
	}
}

// On a case-insensitive filesystem (macOS and Windows defaults), photo.JPG
// and photo.jpg are the same file. The source must be protected there and
// converted normally on case-sensitive filesystems.
func TestCaseInsensitiveAlias(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "photo.JPG")
	orig := writeSource(t, src, format.HEIC)
	_, statErr := os.Stat(filepath.Join(dir, "photo.jpg"))
	caseInsensitive := statErr == nil

	_, err := New(&fakeCodec{}).Convert(context.Background(), Request{SourcePath: src, Format: format.JPEG, Overwrite: true})
	if caseInsensitive {
		if !errors.Is(err, ErrSourceIsOutput) {
			t.Fatalf("case-insensitive filesystem: err = %v; want ErrSourceIsOutput", err)
		}
	} else if err != nil {
		t.Fatalf("case-sensitive filesystem: %v", err)
	}
	assertUnchanged(t, src, orig)
}

func TestSourceErrors(t *testing.T) {
	dir := t.TempDir()
	cv := New(&fakeCodec{})
	ctx := context.Background()

	notImage := filepath.Join(dir, "notes.heic")
	os.WriteFile(notImage, []byte("just text"), 0o644)
	empty := filepath.Join(dir, "empty.heic")
	os.WriteFile(empty, nil, 0o644)
	subdir := filepath.Join(dir, "folder.heic")
	os.Mkdir(subdir, 0o755)

	tests := []struct {
		src  string
		want error
	}{
		{filepath.Join(dir, "missing.heic"), ErrNotFound},
		{notImage, ErrUnsupportedInput},
		{empty, ErrUnsupportedInput},
		{subdir, ErrUnsupportedInput},
	}
	for _, tt := range tests {
		_, err := cv.Convert(ctx, Request{SourcePath: tt.src, Format: format.JPEG})
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v; want %v", filepath.Base(tt.src), err, tt.want)
		}
	}
}

func TestInvalidOptions(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "photo.heic")
	writeSource(t, src, format.HEIC)
	cv := New(&fakeCodec{})
	for name, req := range map[string]Request{
		"no source":        {Format: format.JPEG},
		"heic output":      {SourcePath: src, Format: format.HEIC},
		"unknown format":   {SourcePath: src, Format: "gif"},
		"quality too low":  {SourcePath: src, Format: format.JPEG, Quality: -1},
		"quality too high": {SourcePath: src, Format: format.JPEG, Quality: 101},
		"negative limit":   {SourcePath: src, Format: format.JPEG, MaxPixels: -1},
	} {
		if _, err := cv.Convert(context.Background(), req); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("%s: err = %v; want ErrInvalidOptions", name, err)
		}
	}
}

func TestCodecErrorsClassified(t *testing.T) {
	tests := []struct {
		codecErr error
		want     error
	}{
		{fmt.Errorf("%w: bad data", codecs.ErrDecode), ErrCorrupt},
		{codecs.ErrMultipleImages, ErrUnsupportedInput},
		{fmt.Errorf("%w (PQ)", codecs.ErrHDR), ErrUnsupportedInput},
		{codecs.ErrColourSpace, ErrUnsupportedInput},
		{codecs.ErrTooLarge, ErrUnsupportedInput},
		{fmt.Errorf("%w: disk full", codecs.ErrConversion), ErrConversionFailed},
		{errors.New("something else"), ErrConversionFailed},
		// Source I/O causes win over codec kinds.
		{fmt.Errorf("%w: %w", codecs.ErrDecode, fs.ErrPermission), ErrPermission},
		{fmt.Errorf("%w: %w", codecs.ErrDecode, fs.ErrNotExist), ErrNotFound},
		{fmt.Errorf("vips: %w", fs.ErrPermission), ErrPermission},
	}
	for _, tt := range tests {
		dir := t.TempDir()
		src := filepath.Join(dir, "photo.heic")
		writeSource(t, src, format.HEIC)
		fc := &fakeCodec{convert: func(_ context.Context, _ *os.File, tmp string, _ codecs.Options) error {
			os.WriteFile(tmp, []byte("partial"), 0o644)
			return tt.codecErr
		}}
		_, err := New(fc).Convert(context.Background(), Request{SourcePath: src, Format: format.JPEG})
		if !errors.Is(err, tt.want) {
			t.Errorf("codec %v: err = %v; want %v", tt.codecErr, err, tt.want)
		}
		if !errors.Is(err, tt.codecErr) {
			t.Errorf("codec cause %v not wrapped in %v", tt.codecErr, err)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "photo.jpg")); !os.IsNotExist(statErr) {
			t.Errorf("codec %v: partial output published", tt.codecErr)
		}
		assertNoTempFiles(t, dir)
	}
}

func TestCancellation(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "photo.heic")
	writeSource(t, src, format.HEIC)

	t.Run("before start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		fc := &fakeCodec{}
		_, err := New(fc).Convert(ctx, Request{SourcePath: src, Format: format.JPEG})
		if !errors.Is(err, ErrCancelled) || !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v; want ErrCancelled wrapping context.Canceled", err)
		}
		if fc.calls.Load() != 0 {
			t.Error("codec ran")
		}
	})

	t.Run("during conversion", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		fc := &fakeCodec{convert: func(_ context.Context, _ *os.File, tmp string, _ codecs.Options) error {
			cancel() // the codec finishes, but the caller has gone away
			return os.WriteFile(tmp, []byte("done"), 0o644)
		}}
		_, err := New(fc).Convert(ctx, Request{SourcePath: src, Format: format.JPEG})
		if !errors.Is(err, ErrCancelled) {
			t.Fatalf("err = %v; want ErrCancelled", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "photo.jpg")); !os.IsNotExist(err) {
			t.Error("output published after cancellation")
		}
		assertNoTempFiles(t, dir)
	})
}

// Two jobs targeting the same output without Overwrite: exactly one wins and
// the other reports ErrOutputExists.
func TestConcurrentSameOutput(t *testing.T) {
	dir := t.TempDir()
	srcA := filepath.Join(dir, "a", "photo.heic")
	srcB := filepath.Join(dir, "b", "photo.heic")
	writeSource(t, srcA, format.HEIC)
	writeSource(t, srcB, format.HEIC)
	out := filepath.Join(dir, "out")
	os.Mkdir(out, 0o755)

	start := make(chan struct{})
	fc := &fakeCodec{convert: func(_ context.Context, _ *os.File, tmp string, _ codecs.Options) error {
		<-start // both jobs pass the existence check before either publishes
		return os.WriteFile(tmp, []byte("x"), 0o644)
	}}
	cv := New(fc)
	errs := make(chan error, 2)
	for _, src := range []string{srcA, srcB} {
		go func() {
			_, err := cv.Convert(context.Background(), Request{SourcePath: src, Format: format.JPEG, OutputDir: out})
			errs <- err
		}()
	}
	for fc.calls.Load() < 2 {
		runtime.Gosched()
	}
	close(start)

	var ok, exists int
	for range 2 {
		switch err := <-errs; {
		case err == nil:
			ok++
		case errors.Is(err, ErrOutputExists):
			exists++
		default:
			t.Error(err)
		}
	}
	if ok != 1 || exists != 1 {
		t.Errorf("ok=%d exists=%d; want exactly one of each", ok, exists)
	}
	assertNoTempFiles(t, out)
}

func TestEveryErrorHasOneKind(t *testing.T) {
	for _, err := range []error{
		fsError(os.ErrNotExist), fsError(os.ErrPermission), fsError(errors.New("io")),
		codecError(codecs.ErrDecode), codecError(codecs.ErrHDR), codecError(context.Canceled),
		codecError(errors.New("x")),
	} {
		n := 0
		for _, k := range kinds {
			if errors.Is(err, k) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%v matches %d kinds; want 1", err, n)
		}
	}
}

func TestFSError(t *testing.T) {
	eio := errors.New("input/output error")
	tests := []struct {
		in   error
		want error
	}{
		{fs.ErrNotExist, ErrNotFound},
		{&fs.PathError{Op: "open", Path: "x", Err: fs.ErrPermission}, ErrPermission},
		{fmt.Errorf("wrapped: %w", filesystem.ErrNotRegular), ErrUnsupportedInput},
		{eio, ErrConversionFailed},
		{fmt.Errorf("%w: already classified", ErrCorrupt), ErrCorrupt},
	}
	for _, tt := range tests {
		got := FSError(tt.in)
		if !errors.Is(got, tt.want) || !errors.Is(got, tt.in) {
			t.Errorf("FSError(%v) = %v; want %v wrapping the original", tt.in, got, tt.want)
		}
	}
	if FSError(nil) != nil {
		t.Error("FSError(nil) != nil")
	}
	// An already-classified error is not wrapped again.
	if e := fmt.Errorf("%w: x", ErrCorrupt); FSError(e) != e {
		t.Error("classified error was re-wrapped")
	}
}
