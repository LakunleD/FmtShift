package cli

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lakunled/fmtshift/internal/app"
	"github.com/lakunled/fmtshift/internal/format"
)

// fixtures is absolute because tests change into temporary directories.
var fixtures = func() string {
	p, err := filepath.Abs(filepath.Join("..", "..", "testdata", "images"))
	if err != nil {
		panic(err)
	}
	return p
}()

type result struct {
	code           int
	stdout, stderr string
}

func run(t *testing.T, ctx context.Context, newApp func() (*app.App, error), args ...string) result {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run(ctx, args, &out, &errOut, newApp)
	return result{code, out.String(), errOut.String()}
}

// fmtshift runs the CLI with the real codec.
func fmtshift(t *testing.T, args ...string) result {
	t.Helper()
	return run(t, context.Background(), app.New, args...)
}

func brokenApp() (*app.App, error) { return nil, errors.New("libvips missing") }

// workdir copies fixtures into a temp dir and changes into it, so paths in
// output match what a user would type.
func workdir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, fixture := range files {
		b, err := os.ReadFile(filepath.Join(fixtures, fixture))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	return dir
}

// copyFixture copies a fixture to dest, relative to the current directory.
func copyFixture(t *testing.T, fixture, dest string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtures, fixture))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func decodePNGorJPEG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func (r result) expect(t *testing.T, code int) {
	t.Helper()
	if r.code != code {
		t.Fatalf("exit = %d; want %d\nstdout:\n%s\nstderr:\n%s", r.code, code, r.stdout, r.stderr)
	}
}

func TestVersion(t *testing.T) {
	r := fmtshift(t, "version")
	r.expect(t, ExitOK)
	for _, want := range []string{"FmtShift ", "Platform: ", "Codec: libvips "} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, r.stdout)
		}
	}

	// Version must still work when the image library cannot load.
	r = run(t, context.Background(), brokenApp, "version")
	r.expect(t, ExitOK)
	if !strings.Contains(r.stdout, "Codec: unavailable (libvips missing)") {
		t.Errorf("stdout:\n%s", r.stdout)
	}
}

func TestFormats(t *testing.T) {
	r := fmtshift(t, "formats")
	r.expect(t, ExitOK)
	want := "FORMAT    READ    WRITE\nHEIC      Yes     No\nJPEG      Yes     Yes\nPNG       Yes     Yes\nWEBP      Yes     Yes\n"
	if r.stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, want)
	}
}

func TestConvertSingle(t *testing.T) {
	dir := workdir(t, map[string]string{"IMG 0001.HEIC": "heic/gradient.heic"})

	r := fmtshift(t, "convert", "IMG 0001.HEIC")
	r.expect(t, ExitOK)
	if r.stdout != "Converted IMG 0001.HEIC -> IMG 0001.jpg\n" || r.stderr != "" {
		t.Errorf("stdout=%q stderr=%q", r.stdout, r.stderr)
	}
	if f, err := format.DetectFile(filepath.Join(dir, "IMG 0001.jpg")); err != nil || f != format.JPEG {
		t.Errorf("output format = %q, %v", f, err)
	}

	// Re-running is safe: the existing output is skipped, not an error.
	r = fmtshift(t, "convert", "IMG 0001.HEIC")
	r.expect(t, ExitOK)
	if !strings.Contains(r.stdout, "Skipped IMG 0001.HEIC: IMG 0001.jpg already exists (use --overwrite") {
		t.Errorf("stdout=%q", r.stdout)
	}

	r = fmtshift(t, "convert", "IMG 0001.HEIC", "--overwrite")
	r.expect(t, ExitOK)
	if !strings.HasPrefix(r.stdout, "Converted ") {
		t.Errorf("stdout=%q", r.stdout)
	}
}

func TestConvertOptions(t *testing.T) {
	dir := workdir(t, map[string]string{"photo.heic": "heic/display-p3.heic"})
	r := fmtshift(t, "convert", "photo.heic", "-t", "webp", "-o", "out/nested", "-q", "70", "--strip-metadata")
	r.expect(t, ExitOK)
	out := filepath.Join(dir, "out", "nested", "photo.webp")
	if f, err := format.DetectFile(out); err != nil || f != format.WebP {
		t.Fatalf("output %s: %q, %v", out, f, err)
	}
	if !strings.Contains(r.stdout, filepath.Join("out", "nested", "photo.webp")) {
		t.Errorf("stdout=%q", r.stdout)
	}
}

func TestConvertMixedBatch(t *testing.T) {
	workdir(t, map[string]string{
		"a.heic":      "heic/gradient.heic",
		"hdr.heic":    "heic/hdr-pq.heic",
		"photo.jpg":   "jpeg/gradient.jpg",
		"broken.heic": "invalid/truncated.heic",
		"notes.heic":  "invalid/text.jpg",
	})
	os.Mkdir("folder", 0o755)

	r := fmtshift(t, "convert", "a.heic", "hdr.heic", "photo.jpg", "broken.heic", "notes.heic", "missing.heic", "folder")
	r.expect(t, ExitFailed)

	for _, want := range []string{
		"Converted a.heic -> a.jpg",
		"Skipped photo.jpg: already JPEG",
		"Conversion complete",
		"Converted: 1", "Skipped:   1", "Failed:    5",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, r.stdout)
		}
	}
	for _, want := range []string{
		"Error: hdr.heic: HDR images (PQ/HLG) are not supported yet",
		"Error: broken.heic: the file is corrupt or not a valid image",
		"Error: notes.heic: not a supported image (expected HEIC, JPEG, PNG or WebP)",
		"Error: missing.heic: no such file or directory",
		"Error: folder: is a directory",
	} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, r.stderr)
		}
	}
	// Errors never go to stdout, results never to stderr.
	if strings.Contains(r.stdout, "Error:") || strings.Contains(r.stderr, "Converted") {
		t.Errorf("stdout/stderr mixed:\nstdout:\n%s\nstderr:\n%s", r.stdout, r.stderr)
	}
}

func TestConvertNoEligibleInput(t *testing.T) {
	workdir(t, map[string]string{"a.jpg": "jpeg/gradient.jpg", "b.jpg": "jpeg/gradient.jpg"})
	r := fmtshift(t, "convert", "a.jpg", "b.jpg", "--to", "jpg")
	r.expect(t, ExitNoEligible)
}

func TestSourceIsOutputRefused(t *testing.T) {
	dir := workdir(t, map[string]string{"photo.jpg": "heic/gradient.heic"}) // HEIC named .jpg
	orig, _ := os.ReadFile(filepath.Join(dir, "photo.jpg"))
	r := fmtshift(t, "convert", "photo.jpg", "--overwrite")
	r.expect(t, ExitFailed)
	if !strings.Contains(r.stderr, "would replace the source file itself; use --output") {
		t.Errorf("stderr=%q", r.stderr)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "photo.jpg")); !bytes.Equal(b, orig) {
		t.Fatal("source modified")
	}
}

func TestQuietAndVerbose(t *testing.T) {
	workdir(t, map[string]string{"a.heic": "heic/gradient.heic", "bad.heic": "invalid/truncated.heic"})

	r := fmtshift(t, "--quiet", "convert", "a.heic", "bad.heic")
	r.expect(t, ExitFailed)
	if r.stdout != "" {
		t.Errorf("--quiet printed to stdout: %q", r.stdout)
	}
	if !strings.Contains(r.stderr, "Error: bad.heic") {
		t.Errorf("--quiet hid an error: %q", r.stderr)
	}

	r = fmtshift(t, "convert", "-v", "bad.heic")
	r.expect(t, ExitFailed)
	if !strings.Contains(r.stderr, "detail: corrupt or unreadable image") {
		t.Errorf("--verbose did not show detail: %q", r.stderr)
	}

	fmtshift(t, "--verbose", "--quiet", "formats").expect(t, ExitUsage)
}

func TestUsageErrors(t *testing.T) {
	workdir(t, map[string]string{"a.heic": "heic/gradient.heic"})
	for name, args := range map[string][]string{
		"no command":       {"bogus"},
		"no files":         {"convert"},
		"bad quality":      {"convert", "a.heic", "--quality", "101"},
		"zero quality":     {"convert", "a.heic", "-q", "0"},
		"unknown format":   {"convert", "a.heic", "--to", "gif"},
		"unwritable":       {"convert", "a.heic", "--to", "heic"},
		"unknown flag":     {"convert", "a.heic", "--recursive"},
		"formats with arg": {"formats", "x"},
	} {
		t.Run(name, func(t *testing.T) {
			r := fmtshift(t, args...)
			r.expect(t, ExitUsage)
			if !strings.Contains(r.stderr, "Run 'fmtshift --help'") || r.stdout != "" {
				t.Errorf("stdout=%q stderr=%q", r.stdout, r.stderr)
			}
		})
	}
	if _, err := os.Stat("a.jpg"); err == nil {
		t.Error("a usage error still converted a file")
	}
}

// Inputs that map to the same output must not silently replace each other.
func TestDuplicateDestinations(t *testing.T) {
	t.Run("same name into one output dir with overwrite", func(t *testing.T) {
		dir := workdir(t, nil)
		for _, sub := range []string{"a", "b"} {
			os.Mkdir(sub, 0o755)
		}
		copyFixture(t, "heic/gradient.heic", "a/photo.heic")
		copyFixture(t, "heic/irot90.heic", "b/photo.heic")

		r := fmtshift(t, "convert", "a/photo.heic", "b/photo.heic", "-o", "out", "--overwrite")
		r.expect(t, ExitFailed)
		if !strings.Contains(r.stderr, "Error: b/photo.heic: would write the same output as a/photo.heic") {
			t.Errorf("stderr=%q", r.stderr)
		}
		if !strings.Contains(r.stdout, "Converted: 1") || !strings.Contains(r.stdout, "Failed:    1") {
			t.Errorf("summary:\n%s", r.stdout)
		}
		// The surviving output is the first input's (landscape), not the
		// second's (portrait) written over it.
		img := decodePNGorJPEG(t, filepath.Join(dir, "out", "photo.jpg"))
		if b := img.Bounds(); b.Dx() != 64 || b.Dy() != 32 {
			t.Errorf("out/photo.jpg is %dx%d; the second input replaced the first", b.Dx(), b.Dy())
		}
	})

	t.Run("same stem different format in one folder", func(t *testing.T) {
		workdir(t, map[string]string{"IMG_1.HEIC": "heic/gradient.heic", "IMG_1.PNG": "png/alpha.png"})
		r := fmtshift(t, "convert", "IMG_1.HEIC", "IMG_1.PNG")
		r.expect(t, ExitFailed)
		if !strings.Contains(r.stderr, "Error: IMG_1.PNG: would write the same output as IMG_1.HEIC") {
			t.Errorf("stderr=%q", r.stderr)
		}
		// Not misreported as "already exists".
		if strings.Contains(r.stdout, "already exists") {
			t.Errorf("duplicate misreported as existing output:\n%s", r.stdout)
		}
	})

	t.Run("each converts when given separate output dirs", func(t *testing.T) {
		workdir(t, map[string]string{"IMG_1.HEIC": "heic/gradient.heic"})
		copyFixture(t, "png/alpha.png", "IMG_1.PNG")
		fmtshift(t, "convert", "IMG_1.HEIC", "-o", "from-heic").expect(t, ExitOK)
		fmtshift(t, "convert", "IMG_1.PNG", "-o", "from-png").expect(t, ExitOK)
	})
}

// --quiet hides progress, not the output a command exists to produce.
func TestQuietScope(t *testing.T) {
	r := fmtshift(t, "--quiet", "formats")
	r.expect(t, ExitOK)
	if !strings.HasPrefix(r.stdout, "FORMAT") {
		t.Errorf("--quiet formats printed %q", r.stdout)
	}
	r = fmtshift(t, "--quiet", "version")
	r.expect(t, ExitOK)
	if !strings.HasPrefix(r.stdout, "FmtShift ") {
		t.Errorf("--quiet version printed %q", r.stdout)
	}
}

func TestInterrupted(t *testing.T) {
	dir := workdir(t, map[string]string{"a.heic": "heic/gradient.heic", "b.heic": "heic/gradient.heic"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := run(t, ctx, app.New, "convert", "a.heic", "b.heic")
	r.expect(t, ExitInterrupted)
	if !strings.Contains(r.stderr, "Interrupted.") || !strings.Contains(r.stdout, "Cancelled: 2") {
		t.Errorf("stdout=%q stderr=%q", r.stdout, r.stderr)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("directory has %d entries; want only the 2 sources", len(entries))
	}
}

func TestImageLibraryUnavailable(t *testing.T) {
	workdir(t, map[string]string{"a.heic": "heic/gradient.heic"})
	for _, args := range [][]string{{"convert", "a.heic"}, {"formats"}} {
		r := run(t, context.Background(), brokenApp, args...)
		r.expect(t, ExitFailed)
		if !strings.Contains(r.stderr, "cannot start the image library: libvips missing") {
			t.Errorf("%v: stderr=%q", args, r.stderr)
		}
	}
}
