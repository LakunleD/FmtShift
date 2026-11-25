package converter

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/lakunled/fmtshift/internal/format"
)

func TestDuplicateDestinations(t *testing.T) {
	dir := t.TempDir()
	write := func(rel string, f format.Format) string {
		p := filepath.Join(dir, rel)
		writeSource(t, p, f)
		return p
	}
	heicA := write("a/photo.heic", format.HEIC)
	heicB := write("b/photo.heic", format.HEIC)
	heicUpper := write("c/PHOTO.HEIC", format.HEIC)
	img1Heic := write("same/IMG_1.HEIC", format.HEIC)
	img1Png := write("same/IMG_1.PNG", format.PNG)
	img2 := write("same/IMG_2.HEIC", format.HEIC)
	jpg := write("same/IMG_1.JPG", format.JPEG) // same format as target: no output
	out := filepath.Join(dir, "out")
	link := filepath.Join(dir, "out-link")
	os.Mkdir(out, 0o755)
	os.Symlink(out, link)

	jpeg := func(src, outDir string) Request {
		return Request{SourcePath: src, Format: format.JPEG, OutputDir: outDir}
	}
	cv := New(&fakeCodec{})

	tests := []struct {
		name string
		reqs []Request
		want map[int]int
	}{
		{"distinct", []Request{jpeg(heicA, ""), jpeg(heicB, "")}, map[int]int{}},
		{"same name into one dir", []Request{jpeg(heicA, out), jpeg(heicB, out)}, map[int]int{1: 0}},
		{"different case into one dir", []Request{jpeg(heicA, out), jpeg(heicUpper, out)}, map[int]int{1: 0}},
		{"same stem, different format", []Request{jpeg(img1Heic, ""), jpeg(img2, ""), jpeg(img1Png, "")}, map[int]int{2: 0}},
		{"output dir via symlink", []Request{jpeg(heicA, out), jpeg(heicB, link)}, map[int]int{1: 0}},
		{"same file twice", []Request{jpeg(heicA, ""), jpeg(heicA, "")}, map[int]int{1: 0}},
		{"same-format input ignored", []Request{jpeg(img1Heic, ""), jpeg(jpg, "")}, map[int]int{}},
		{"missing input ignored", []Request{jpeg(heicA, out), jpeg(filepath.Join(dir, "a/missing.heic"), out)}, map[int]int{}},
		{"three-way", []Request{jpeg(heicA, out), jpeg(heicB, out), jpeg(heicUpper, out)}, map[int]int{1: 0, 2: 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cv.DuplicateDestinations(tt.reqs); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v; want %v", got, tt.want)
			}
		})
	}
}

// Destination must agree with the path Convert actually writes.
func TestDestinationMatchesConvert(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "IMG 7.HEIC")
	writeSource(t, src, format.HEIC)
	cv := New(&fakeCodec{})
	for _, req := range []Request{
		{SourcePath: src, Format: format.PNG},
		{SourcePath: src, Format: format.WebP, OutputDir: filepath.Join(dir, "out"), CreateOutputDir: true},
	} {
		want, ok := cv.Destination(req)
		if !ok {
			t.Fatal("Destination reported no output")
		}
		res, err := cv.Convert(t.Context(), req)
		if err != nil {
			t.Fatal(err)
		}
		if res.OutputPath != want {
			t.Errorf("Convert wrote %q; Destination predicted %q", res.OutputPath, want)
		}
	}
}
