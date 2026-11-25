package batch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/converter"
	"github.com/lakunled/fmtshift/internal/format"
)

// fakeCodec writes a marker file, optionally slowly, and records the peak
// number of concurrent conversions.
type fakeCodec struct {
	delay  time.Duration
	failOn string // fail sources whose name contains this
	active atomic.Int32
	peak   atomic.Int32
	calls  atomic.Int32
	before func() // runs at the start of each conversion
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

func (f *fakeCodec) Convert(_ context.Context, src *os.File, _ format.Format, dst string, _ format.Format, _ codecs.Options) error {
	f.calls.Add(1)
	if f.before != nil {
		f.before()
	}
	n := f.active.Add(1)
	defer f.active.Add(-1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	time.Sleep(f.delay)
	if f.failOn != "" && strings.Contains(src.Name(), f.failOn) {
		return codecs.ErrDecode
	}
	return os.WriteFile(dst, []byte("converted"), 0o644)
}

var signatures = map[format.Format]string{
	format.HEIC: "\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic",
	format.JPEG: "\xFF\xD8\xFF\xE0",
	format.PNG:  "\x89PNG\r\n\x1a\n",
	format.WebP: "RIFF\x00\x00\x00\x00WEBPVP8 ",
}

// tree creates files under root: name -> format ("" for a non-image).
func tree(t *testing.T, root string, files map[string]format.Format) {
	t.Helper()
	for name, f := range files {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		content := "not an image"
		if f != "" {
			content = signatures[f] + "pixels"
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func jpegTemplate(out string) converter.Request {
	return converter.Request{Format: format.JPEG, Quality: 85, OutputDir: out}
}

// byName indexes items by source path relative to root.
func byName(t *testing.T, root string, items []*Item) map[string]*Item {
	t.Helper()
	m := map[string]*Item{}
	for _, it := range items {
		rel, err := filepath.Rel(root, it.Source)
		if err != nil {
			rel = it.Source
		}
		m[filepath.ToSlash(rel)] = it
	}
	return m
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	tree(t, root, map[string]format.Format{
		"a.heic":                     format.HEIC,
		"b.png":                      format.PNG,
		"c.jpg":                      format.JPEG,
		"notes.txt":                  "",
		".hidden.heic":               format.HEIC,
		".photo.jpg.x1.fmtshift-tmp": format.JPEG,
		".git/objects/x.heic":        format.HEIC,
		"sub/d.heic":                 format.HEIC,
		"sub/deeper/e.heic":          format.HEIC,
	})

	t.Run("non-recursive", func(t *testing.T) {
		got := byName(t, root, mustDiscover(t, []string{root}, Options{Template: jpegTemplate("")}))
		want := map[string]Outcome{
			"a.heic": Pending, "b.png": Pending,
			"c.jpg": SkippedSameFormat, "notes.txt": SkippedUnsupported,
		}
		assertOutcomes(t, got, want)
		for _, it := range got {
			if it.Explicit || it.Request.OutputDir != "" {
				t.Errorf("%s: explicit=%v outputDir=%q", it.Source, it.Explicit, it.Request.OutputDir)
			}
		}
	})

	t.Run("recursive with output dir recreates structure", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "out")
		got := byName(t, root, mustDiscover(t, []string{root}, Options{Recursive: true, Template: jpegTemplate(out)}))
		assertOutcomes(t, got, map[string]Outcome{
			"a.heic": Pending, "b.png": Pending, "c.jpg": SkippedSameFormat, "notes.txt": SkippedUnsupported,
			"sub/d.heic": Pending, "sub/deeper/e.heic": Pending,
		})
		wantDirs := map[string]string{
			"a.heic":            out,
			"sub/d.heic":        filepath.Join(out, "sub"),
			"sub/deeper/e.heic": filepath.Join(out, "sub", "deeper"),
		}
		for name, dir := range wantDirs {
			if got := got[name].Request.OutputDir; got != dir {
				t.Errorf("%s: OutputDir = %q; want %q", name, got, dir)
			}
			if !got[name].Request.CreateOutputDir {
				t.Errorf("%s: CreateOutputDir not set", name)
			}
		}
	})

	t.Run("from filter", func(t *testing.T) {
		got := byName(t, root, mustDiscover(t, []string{root}, Options{From: []format.Format{format.HEIC}, Template: jpegTemplate("")}))
		assertOutcomes(t, got, map[string]Outcome{
			"a.heic": Pending, "b.png": SkippedFiltered,
			"c.jpg": SkippedFiltered, "notes.txt": SkippedUnsupported,
		})
	})
}

func assertOutcomes(t *testing.T, got map[string]*Item, want map[string]Outcome) {
	t.Helper()
	for name, o := range want {
		it, ok := got[name]
		if !ok {
			t.Errorf("%s not discovered", name)
			continue
		}
		if it.Outcome != o {
			t.Errorf("%s: outcome %d; want %d", name, it.Outcome, o)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("unexpected item %s", name)
		}
	}
}

func TestDiscoverExplicitFiles(t *testing.T) {
	root := t.TempDir()
	tree(t, root, map[string]format.Format{"a.png": format.PNG, "notes.txt": ""})
	items := mustDiscover(t, []string{
		filepath.Join(root, "a.png"), filepath.Join(root, "notes.txt"), filepath.Join(root, "missing.heic"),
	}, Options{From: []format.Format{format.HEIC}, Template: jpegTemplate("")})

	// Explicit files are always items; the engine reports their problems.
	if len(items) != 3 {
		t.Fatalf("got %d items", len(items))
	}
	if items[0].Outcome != SkippedFiltered || !items[0].Explicit {
		t.Errorf("a.png: %+v", items[0])
	}
	if items[1].Outcome != Pending || items[2].Outcome != Pending {
		t.Errorf("non-image and missing explicit files must reach the engine: %v, %v", items[1].Outcome, items[2].Outcome)
	}
}

func TestDiscoverSymlinks(t *testing.T) {
	root := t.TempDir()
	tree(t, root, map[string]format.Format{"real/a.heic": format.HEIC, "elsewhere/b.heic": format.HEIC})
	in := filepath.Join(root, "real")
	if err := os.Symlink(filepath.Join(root, "elsewhere"), filepath.Join(in, "linked-dir")); err != nil {
		t.Skip(err)
	}
	os.Symlink(filepath.Join(root, "elsewhere", "b.heic"), filepath.Join(in, "linked.heic"))
	os.Symlink(in, filepath.Join(in, "loop")) // would recurse forever if followed

	got := byName(t, in, mustDiscover(t, []string{in}, Options{Recursive: true, Template: jpegTemplate("")}))
	assertOutcomes(t, got, map[string]Outcome{"a.heic": Pending, "linked.heic": Pending})
}

// With the output directory inside the input tree, a re-run must not pick
// up its own outputs.
func TestDiscoverSkipsOutputDirInsideInput(t *testing.T) {
	root := t.TempDir()
	tree(t, root, map[string]format.Format{"a.heic": format.HEIC, "converted/old.heic": format.HEIC})
	got := byName(t, root, mustDiscover(t, []string{root}, Options{
		Recursive: true, Template: jpegTemplate(filepath.Join(root, "converted")),
	}))
	assertOutcomes(t, got, map[string]Outcome{"a.heic": Pending})
}

func run(t *testing.T, fc *fakeCodec, ctx context.Context, items []*Item, workers int) []*Item {
	t.Helper()
	var reported []*Item
	Run(ctx, converter.New(fc), items, workers, func(it *Item) { reported = append(reported, it) })
	return reported
}

func TestRunWorkerLimit(t *testing.T) {
	root := t.TempDir()
	files := map[string]format.Format{}
	for i := range 12 {
		files[filepath.Join("p", string(rune('a'+i))+".heic")] = format.HEIC
	}
	tree(t, root, files)

	for _, workers := range []int{1, 3} {
		fc := &fakeCodec{delay: 30 * time.Millisecond}
		items := mustDiscover(t, []string{filepath.Join(root, "p")}, Options{Template: converter.Request{Format: format.PNG, OutputDir: t.TempDir()}})
		reported := run(t, fc, context.Background(), items, workers)
		if got := fc.peak.Load(); got != int32(workers) {
			t.Errorf("workers=%d: peak concurrency %d", workers, got)
		}
		if len(reported) != len(items) {
			t.Errorf("reported %d of %d items", len(reported), len(items))
		}
		if s := Summarize(items); s[Converted] != 12 {
			t.Errorf("converted %d; want 12", s[Converted])
		}
	}
}

func TestRunFailuresAreIndependent(t *testing.T) {
	root := t.TempDir()
	tree(t, root, map[string]format.Format{"a.heic": format.HEIC, "bad.heic": format.HEIC, "c.heic": format.HEIC})
	fc := &fakeCodec{failOn: "bad"}
	items := mustDiscover(t, []string{root}, Options{Template: jpegTemplate("")})
	run(t, fc, context.Background(), items, 2)
	got := byName(t, root, items)
	if got["a.heic"].Outcome != Converted || got["c.heic"].Outcome != Converted {
		t.Errorf("good files not converted: %v %v", got["a.heic"].Outcome, got["c.heic"].Outcome)
	}
	if it := got["bad.heic"]; it.Outcome != Failed || !errors.Is(it.Err, converter.ErrCorrupt) {
		t.Errorf("bad.heic: %v %v", it.Outcome, it.Err)
	}
}

func TestRunCancellation(t *testing.T) {
	root := t.TempDir()
	files := map[string]format.Format{}
	for i := range 10 {
		files[string(rune('a'+i))+".heic"] = format.HEIC
	}
	tree(t, root, files)

	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	fc := &fakeCodec{delay: 10 * time.Millisecond, before: func() { once.Do(cancel) }}
	items := mustDiscover(t, []string{root}, Options{Template: jpegTemplate("")})
	reported := run(t, fc, ctx, items, 1)

	s := Summarize(items)
	if s[Cancelled] != 10 || s[Converted] != 0 {
		t.Errorf("summary %v; want all 10 cancelled", s)
	}
	if len(reported) != 10 {
		t.Errorf("reported %d items", len(reported))
	}
	if fc.calls.Load() != 1 {
		t.Errorf("codec ran %d times after cancellation; want 1 (the in-flight one)", fc.calls.Load())
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".heic") {
			t.Errorf("unexpected file after cancellation: %s", e.Name())
		}
	}
}

func TestRunDuplicatesAcrossDirectories(t *testing.T) {
	root := t.TempDir()
	tree(t, root, map[string]format.Format{"a/photo.heic": format.HEIC, "b/photo.heic": format.HEIC, "b/other.heic": format.HEIC})
	out := filepath.Join(root, "out")
	fc := &fakeCodec{}
	items := mustDiscover(t, []string{filepath.Join(root, "a"), filepath.Join(root, "b")}, Options{Template: jpegTemplate(out)})
	run(t, fc, context.Background(), items, 2)

	got := byName(t, root, items)
	if got["a/photo.heic"].Outcome != Converted || got["b/other.heic"].Outcome != Converted {
		t.Errorf("non-duplicates: %v %v", got["a/photo.heic"].Outcome, got["b/other.heic"].Outcome)
	}
	dup := got["b/photo.heic"]
	if dup.Outcome != Failed || !errors.Is(dup.Err, ErrDuplicate) || dup.ConflictsWith != filepath.Join(root, "a", "photo.heic") {
		t.Errorf("duplicate: outcome=%v err=%v of=%q", dup.Outcome, dup.Err, dup.ConflictsWith)
	}
	if fc.calls.Load() != 2 {
		t.Errorf("codec ran %d times; duplicates must not be converted", fc.calls.Load())
	}
}

func TestReportOrder(t *testing.T) {
	// Items resolved during discovery are reported first, in order.
	root := t.TempDir()
	tree(t, root, map[string]format.Format{"a.heic": format.HEIC, "b.jpg": format.JPEG, "c.txt": ""})
	items := mustDiscover(t, []string{root}, Options{Template: jpegTemplate("")})
	reported := run(t, &fakeCodec{}, context.Background(), items, 1)
	var names []string
	for _, it := range reported {
		names = append(names, filepath.Base(it.Source))
	}
	if !slices.Equal(names, []string{"b.jpg", "c.txt", "a.heic"}) {
		t.Errorf("report order %v", names)
	}
}

func TestSummary(t *testing.T) {
	s := Summarize([]*Item{{Outcome: SkippedSameFormat}, {Outcome: SkippedUnsupported}, {Outcome: Failed}})
	if s.Productive() || s.Skipped() != 2 {
		t.Errorf("summary %v: productive=%v skipped=%d", s, s.Productive(), s.Skipped())
	}
	if !Summarize([]*Item{{Outcome: SkippedExists}}).Productive() {
		t.Error("an existing output counts as eligible work")
	}
}

func mustDiscover(t *testing.T, inputs []string, opt Options) []*Item {
	t.Helper()
	items, err := Discover(t.Context(), inputs, opt)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// A cancelled scan stops without walking the tree and reports the
// interruption instead of a (possibly empty, all-skipped) partial result.
func TestDiscoverCancelled(t *testing.T) {
	root := t.TempDir()
	files := map[string]format.Format{}
	for i := range 50 {
		files[filepath.Join(fmt.Sprintf("d%02d", i), "x.heic")] = format.HEIC
	}
	tree(t, root, files)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	items, err := Discover(ctx, []string{root, filepath.Join(root, "d00", "x.heic")}, Options{Recursive: true, Template: jpegTemplate("")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v; want context.Canceled", err)
	}
	if len(items) != 0 {
		t.Errorf("found %d items after cancellation; want 0", len(items))
	}
}

// Cancelling mid-walk stops the walk at the next entry.
func TestDiscoverCancelledMidWalk(t *testing.T) {
	root := t.TempDir()
	files := map[string]format.Format{}
	for i := range 50 {
		files[fmt.Sprintf("f%02d.heic", i)] = format.HEIC
	}
	tree(t, root, files)

	ctx, cancel := context.WithCancel(context.Background())
	visited := 0
	testHookVisit = func() {
		if visited++; visited == 3 {
			cancel()
		}
	}
	t.Cleanup(func() { testHookVisit = nil })

	items, err := Discover(ctx, []string{root}, Options{Template: jpegTemplate("")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v; want context.Canceled", err)
	}
	if len(items) != 3 {
		t.Errorf("found %d items; the walk should stop right after the 3rd", len(items))
	}
}

// With --overwrite, an output must never replace another input of the batch,
// even one that is itself skipped (same format, filtered) and so would not
// otherwise be protected.
func TestOverwriteNeverReplacesAnotherInput(t *testing.T) {
	jpegBytes := signatures[format.JPEG] + "pixels"

	cases := map[string]struct {
		from     []format.Format
		explicit bool
	}{
		"folder, same-format sibling":   {},
		"folder, filtered sibling":      {from: []format.Format{format.PNG}},
		"explicit files on the command": {explicit: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			tree(t, root, map[string]format.Format{"photo.jpg": format.JPEG, "photo.png": format.PNG})
			inputs := []string{root}
			if tc.explicit {
				inputs = []string{filepath.Join(root, "photo.png"), filepath.Join(root, "photo.jpg")}
			}
			tmpl := jpegTemplate("")
			tmpl.Overwrite = true
			fc := &fakeCodec{}
			items := mustDiscover(t, inputs, Options{From: tc.from, Template: tmpl})
			run(t, fc, context.Background(), items, 2)

			got := byName(t, root, items)
			png := got["photo.png"]
			if png.Outcome != Failed || !errors.Is(png.Err, ErrReplacesInput) || png.ConflictsWith != filepath.Join(root, "photo.jpg") {
				t.Errorf("photo.png: outcome %v err %v conflicts %q; want ErrReplacesInput naming photo.jpg", png.Outcome, png.Err, png.ConflictsWith)
			}
			if b, _ := os.ReadFile(filepath.Join(root, "photo.jpg")); string(b) != jpegBytes {
				t.Fatalf("original photo.jpg was replaced with %q", b)
			}
			if fc.calls.Load() != 0 {
				t.Errorf("codec ran %d times", fc.calls.Load())
			}
		})
	}
}

// Without --overwrite nothing is replaced, so the same layout is a safe
// skip, and re-runs stay successful.
func TestNoOverwriteSiblingIsSkippedNotFailed(t *testing.T) {
	root := t.TempDir()
	tree(t, root, map[string]format.Format{"photo.jpg": format.JPEG, "photo.png": format.PNG})
	items := mustDiscover(t, []string{root}, Options{Template: jpegTemplate("")})
	run(t, &fakeCodec{}, context.Background(), items, 1)
	got := byName(t, root, items)
	if got["photo.png"].Outcome != SkippedExists {
		t.Errorf("photo.png: outcome %v; want SkippedExists", got["photo.png"].Outcome)
	}
}

// Outputs going elsewhere do not conflict, even with the same names.
func TestOverwriteIntoOtherDirectoryIsAllowed(t *testing.T) {
	root := t.TempDir()
	tree(t, root, map[string]format.Format{"in/photo.jpg": format.JPEG, "in/photo.png": format.PNG})
	tmpl := jpegTemplate(filepath.Join(root, "out"))
	tmpl.Overwrite = true
	items := mustDiscover(t, []string{filepath.Join(root, "in")}, Options{Template: tmpl})
	run(t, &fakeCodec{}, context.Background(), items, 1)
	if got := byName(t, root, items)["in/photo.png"]; got.Outcome != Converted {
		t.Errorf("photo.png: outcome %v err %v; want converted into out/", got.Outcome, got.Err)
	}
}

// An input that is a symlink protects the file it reads, not only its own
// name: replacing the target would change the input's content.
func TestOverwriteNeverReplacesSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	tree(t, root, map[string]format.Format{"a/photo.png": format.PNG, "a/photo.jpg": format.JPEG})
	os.Mkdir(filepath.Join(root, "b"), 0o755)
	if err := os.Symlink(filepath.Join("..", "a", "photo.jpg"), filepath.Join(root, "b", "original.jpg")); err != nil {
		t.Skip(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "b", "original.jpg"))

	tmpl := jpegTemplate("")
	tmpl.Overwrite = true
	fc := &fakeCodec{}
	// a/photo.jpg itself is not an input; only the symlink reaches it.
	items := mustDiscover(t, []string{filepath.Join(root, "a", "photo.png"), filepath.Join(root, "b")}, Options{Template: tmpl})
	run(t, fc, context.Background(), items, 1)

	png := byName(t, root, items)["a/photo.png"]
	if png.Outcome != Failed || !errors.Is(png.Err, ErrReplacesInput) || png.ConflictsWith != filepath.Join(root, "b", "original.jpg") {
		t.Errorf("a/photo.png: outcome %v err %v conflicts %q; want ErrReplacesInput naming b/original.jpg", png.Outcome, png.Err, png.ConflictsWith)
	}
	if after, _ := os.ReadFile(filepath.Join(root, "b", "original.jpg")); !bytes.Equal(before, after) {
		t.Fatal("content read through the symlink input changed")
	}
	if fc.calls.Load() != 0 {
		t.Errorf("codec ran %d times", fc.calls.Load())
	}
}

// A missing input must be reported as missing, not created by another
// input's conversion and then "found", whatever the worker count.
func TestOutputMustNotCreateMissingInput(t *testing.T) {
	for _, workers := range []int{1, 2, 4} {
		for _, overwrite := range []bool{false, true} {
			root := t.TempDir()
			tree(t, root, map[string]format.Format{"photo.png": format.PNG})
			tmpl := jpegTemplate("")
			tmpl.Overwrite = overwrite
			fc := &fakeCodec{}
			items := mustDiscover(t, []string{filepath.Join(root, "photo.png"), filepath.Join(root, "photo.jpg")}, Options{Template: tmpl})
			run(t, fc, context.Background(), items, workers)

			got := byName(t, root, items)
			png, jpg := got["photo.png"], got["photo.jpg"]
			if png.Outcome != Failed || !errors.Is(png.Err, ErrCreatesInput) || png.ConflictsWith != filepath.Join(root, "photo.jpg") {
				t.Errorf("workers=%d overwrite=%v: photo.png outcome %v err %v; want ErrCreatesInput", workers, overwrite, png.Outcome, png.Err)
			}
			if jpg.Outcome != Failed || !errors.Is(jpg.Err, converter.ErrNotFound) {
				t.Errorf("workers=%d overwrite=%v: photo.jpg outcome %v err %v; want not found", workers, overwrite, jpg.Outcome, jpg.Err)
			}
			if _, err := os.Stat(filepath.Join(root, "photo.jpg")); err == nil {
				t.Errorf("workers=%d overwrite=%v: photo.jpg was created", workers, overwrite)
			}
		}
	}
}
