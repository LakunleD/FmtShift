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

	r := fmtshift(t, "convert", "a.heic", "hdr.heic", "photo.jpg", "broken.heic", "notes.heic", "missing.heic")
	r.expect(t, ExitFailed)

	for _, want := range []string{
		"Converted a.heic -> a.jpg",
		"Skipped photo.jpg: already JPEG",
		"Conversion complete",
		"Converted: 1", "Skipped:   1", "Failed:    4",
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
		"unknown flag":     {"convert", "a.heic", "--json"},
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

// photoTree creates a folder of mixed files in the current directory.
func photoTree(t *testing.T) {
	t.Helper()
	for _, d := range []string{"photos/2024/june", "photos/.thumbnails"} {
		os.MkdirAll(d, 0o755)
	}
	copyFixture(t, "heic/gradient.heic", "photos/a.heic")
	copyFixture(t, "png/alpha.png", "photos/b.png")
	copyFixture(t, "jpeg/gradient.jpg", "photos/c.jpg")
	copyFixture(t, "invalid/text.jpg", "photos/notes.txt")
	copyFixture(t, "heic/gradient.heic", "photos/.thumbnails/t.heic")
	copyFixture(t, "heic/irot90.heic", "photos/2024/d.heic")
	copyFixture(t, "heic/gradient.heic", "photos/2024/june/e.heic")
}

func TestConvertDirectory(t *testing.T) {
	dir := workdir(t, nil)
	photoTree(t)

	r := fmtshift(t, "convert", "photos")
	r.expect(t, ExitOK)
	for _, out := range []string{"photos/a.jpg", "photos/b.jpg"} {
		if _, err := os.Stat(filepath.Join(dir, out)); err != nil {
			t.Errorf("%s not created", out)
		}
	}
	for _, absent := range []string{"photos/2024/d.jpg", "photos/.thumbnails/t.jpg"} {
		if _, err := os.Stat(filepath.Join(dir, absent)); err == nil {
			t.Errorf("%s created without --recursive or from a hidden folder", absent)
		}
	}
	// Expected skips in folders are not listed without --verbose.
	if strings.Contains(r.stdout, "c.jpg") || strings.Contains(r.stdout, "notes.txt") {
		t.Errorf("folder skips listed without --verbose:\n%s", r.stdout)
	}
	if !strings.Contains(r.stdout, "Converted: 2") || !strings.Contains(r.stdout, "Skipped:   2") {
		t.Errorf("summary:\n%s", r.stdout)
	}

	r = fmtshift(t, "convert", "photos", "--verbose")
	if !strings.Contains(r.stdout, "Skipped photos/notes.txt: not a supported image") ||
		!strings.Contains(r.stdout, "Skipped photos/c.jpg: already JPEG") {
		t.Errorf("--verbose did not list folder skips:\n%s", r.stdout)
	}
}

func TestConvertRecursiveIntoOutput(t *testing.T) {
	dir := workdir(t, nil)
	photoTree(t)

	r := fmtshift(t, "convert", "photos", "-r", "-o", "out", "--from", "heic", "-w", "3")
	r.expect(t, ExitOK)
	for _, out := range []string{"out/a.jpg", "out/2024/d.jpg", "out/2024/june/e.jpg"} {
		if f, err := format.DetectFile(filepath.Join(dir, out)); err != nil || f != format.JPEG {
			t.Errorf("%s: %q, %v", out, f, err)
		}
	}
	for _, absent := range []string{"out/b.jpg", "out/.thumbnails", "photos/a.jpg"} {
		if _, err := os.Stat(filepath.Join(dir, absent)); err == nil {
			t.Errorf("%s should not exist (--from heic, hidden folder, or wrong location)", absent)
		}
	}

	// Re-running converts nothing new and still succeeds.
	r = fmtshift(t, "convert", "photos", "-r", "-o", "out", "--from", "heic")
	r.expect(t, ExitOK)
	if !strings.Contains(r.stdout, "Converted: 0") {
		t.Errorf("re-run summary:\n%s", r.stdout)
	}
}

func TestConvertNothingToDo(t *testing.T) {
	workdir(t, nil)
	os.Mkdir("empty", 0o755)
	r := fmtshift(t, "convert", "empty")
	r.expect(t, ExitNoEligible)
	if !strings.Contains(r.stderr, "Nothing to convert") {
		t.Errorf("stderr=%q", r.stderr)
	}

	photoTree(t)
	r = fmtshift(t, "convert", "photos", "--from", "webp")
	r.expect(t, ExitNoEligible)
}

func TestBatchFlagValidation(t *testing.T) {
	workdir(t, map[string]string{"a.heic": "heic/gradient.heic"})
	for name, args := range map[string][]string{
		"zero workers":   {"convert", "a.heic", "-w", "0"},
		"too many":       {"convert", "a.heic", "--workers", "1000"},
		"unknown --from": {"convert", "a.heic", "--from", "gif"},
	} {
		t.Run(name, func(t *testing.T) { fmtshift(t, args...).expect(t, ExitUsage) })
	}
}

func TestInterrupted(t *testing.T) {
	dir := workdir(t, map[string]string{"a.heic": "heic/gradient.heic", "b.heic": "heic/gradient.heic"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := run(t, ctx, app.New, "convert", "a.heic", "b.heic")
	r.expect(t, ExitInterrupted)
	// Cancelled before discovery: nothing is scanned, converted or
	// summarised.
	if r.stderr != "Interrupted.\n" || r.stdout != "" {
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

// Interruption wins over "nothing to convert": a Ctrl+C during the scan of a
// folder whose files would all be skipped must exit 130, not 4.
func TestInterruptedScanOfSkippableFolder(t *testing.T) {
	workdir(t, nil)
	os.Mkdir("jpegs", 0o755)
	copyFixture(t, "jpeg/gradient.jpg", "jpegs/a.jpg")
	copyFixture(t, "jpeg/gradient.jpg", "jpegs/b.jpg")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := run(t, ctx, app.New, "convert", "jpegs")
	r.expect(t, ExitInterrupted)
	if strings.Contains(r.stderr, "Nothing to convert") {
		t.Errorf("interrupted scan reported as nothing to convert: %q", r.stderr)
	}
}

// The reviewer's reproduction: a folder with photo.jpg and photo.png,
// converted to JPEG with --overwrite, must not replace the original JPEG.
func TestOverwriteDoesNotReplaceSiblingInput(t *testing.T) {
	dir := workdir(t, nil)
	os.Mkdir("f", 0o755)
	copyFixture(t, "jpeg/gradient.jpg", "f/photo.jpg")
	copyFixture(t, "png/alpha.png", "f/photo.png")
	orig, _ := os.ReadFile(filepath.Join(dir, "f", "photo.jpg"))

	r := fmtshift(t, "convert", "f", "--overwrite")
	r.expect(t, ExitFailed)
	if b, _ := os.ReadFile(filepath.Join(dir, "f", "photo.jpg")); !bytes.Equal(b, orig) {
		t.Fatal("original photo.jpg was replaced")
	}
	if !strings.Contains(r.stderr, "Error: f/photo.png: would replace f/photo.jpg, which is one of the inputs; use --output") {
		t.Errorf("stderr=%q", r.stderr)
	}
	if strings.Contains(r.stdout, "Converted f/photo.png") {
		t.Errorf("reported as converted:\n%s", r.stdout)
	}

	// Into another directory it converts.
	fmtshift(t, "convert", "f", "--overwrite", "-o", "out").expect(t, ExitOK)
}

// An unreadable file found in a folder is reported as permission denied.
func TestUnreadableFileInFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits are not enforced for root")
	}
	workdir(t, nil)
	os.Mkdir("f", 0o755)
	copyFixture(t, "heic/gradient.heic", "f/locked.heic")
	os.Chmod("f/locked.heic", 0)
	t.Cleanup(func() { os.Chmod("f/locked.heic", 0o644) })

	r := fmtshift(t, "convert", "f")
	r.expect(t, ExitFailed)
	if !strings.Contains(r.stderr, "Error: f/locked.heic: permission denied") {
		t.Errorf("stderr=%q", r.stderr)
	}
}

// The reviewer's reproduction: a/photo.png plus b/original.jpg -> a/photo.jpg.
func TestOverwriteDoesNotReplaceSymlinkedInputTarget(t *testing.T) {
	dir := workdir(t, nil)
	os.MkdirAll("a", 0o755)
	os.MkdirAll("b", 0o755)
	copyFixture(t, "png/alpha.png", "a/photo.png")
	copyFixture(t, "jpeg/gradient.jpg", "a/photo.jpg")
	if err := os.Symlink("../a/photo.jpg", "b/original.jpg"); err != nil {
		t.Skip(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "b", "original.jpg"))

	r := fmtshift(t, "convert", "a/photo.png", "b", "--overwrite")
	r.expect(t, ExitFailed)
	if after, _ := os.ReadFile(filepath.Join(dir, "b", "original.jpg")); !bytes.Equal(before, after) {
		t.Fatal("content read through b/original.jpg changed")
	}
	if !strings.Contains(r.stderr, "Error: a/photo.png: would replace a/photo.jpg, which input b/original.jpg refers to; use --output") {
		t.Errorf("stderr=%q", r.stderr)
	}
}

// The reviewer's reproduction: photo.png plus a missing photo.jpg.
func TestMissingInputNotMaskedByAnotherOutput(t *testing.T) {
	workdir(t, map[string]string{"photo.png": "png/alpha.png"})
	for _, w := range []string{"1", "2"} {
		r := fmtshift(t, "convert", "photo.png", "photo.jpg", "--workers", w)
		r.expect(t, ExitFailed)
		if !strings.Contains(r.stderr, "Error: photo.jpg: no such file or directory") {
			t.Errorf("workers=%s: missing input not reported: %q", w, r.stderr)
		}
		if !strings.Contains(r.stderr, "Error: photo.png: its output photo.jpg would take the place of input photo.jpg, which does not exist") {
			t.Errorf("workers=%s: stderr=%q", w, r.stderr)
		}
		if _, err := os.Stat("photo.jpg"); err == nil {
			t.Fatalf("workers=%s: photo.jpg was created", w)
		}
	}
}
