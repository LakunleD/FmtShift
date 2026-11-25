// Package batch discovers input files and converts them with a bounded pool
// of workers. It is independent of any user interface: the CLI and the
// desktop application both drive it and render its Items.
package batch

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/lakunled/fmtshift/internal/converter"
	"github.com/lakunled/fmtshift/internal/filesystem"
	"github.com/lakunled/fmtshift/internal/format"
)

// Outcome is how one input ended up.
type Outcome int

const (
	// Pending items have not been converted yet.
	Pending Outcome = iota
	Converted
	// SkippedExists: the output exists and Overwrite is off. Safe re-runs.
	SkippedExists
	// SkippedSameFormat: already in the target format.
	SkippedSameFormat
	// SkippedUnsupported: found in a directory but not a supported image.
	SkippedUnsupported
	// SkippedFiltered: excluded by the From filter.
	SkippedFiltered
	Failed
	Cancelled
)

var (
	// ErrDuplicate marks an input that would write the same output as an
	// earlier input. Item.ConflictsWith names that input.
	ErrDuplicate = errors.New("same output as another input")
	// ErrReplacesInput marks an input whose output, with Overwrite, would
	// replace another input of the same batch, for example photo.png
	// converted to JPEG next to an original photo.jpg. Item.ConflictsWith
	// names that input.
	ErrReplacesInput = errors.New("output would replace another input")
	// ErrCreatesInput marks an input whose output would be created at the
	// path of another input that does not exist yet, for example photo.png
	// together with a missing photo.jpg. Converting would make the missing
	// input appear and hide the error. Item.ConflictsWith names that input.
	ErrCreatesInput = errors.New("output would create another input")
)

// Item is one input file and its outcome.
type Item struct {
	// Source is the input path, as given or as found under a directory.
	Source string
	// Explicit is true for files named directly rather than found in a
	// directory. Problems with explicit files are failures; unsupported
	// files found in directories are skipped.
	Explicit bool
	Request  converter.Request
	Result   converter.Result
	Outcome  Outcome
	Err      error
	// ConflictsWith is the Source of the other input involved when Err is
	// ErrDuplicate or ErrReplacesInput.
	ConflictsWith string
}

// Options control discovery.
type Options struct {
	// Recursive descends into subdirectories of directory inputs. With an
	// OutputDir, the subdirectory structure is recreated under it.
	Recursive bool
	// From limits inputs to these content formats; empty means all.
	From []format.Format
	// Template supplies every field of each Request except SourcePath.
	// Its OutputDir is the base output directory ("" = next to sources).
	Template converter.Request
}

// Discover expands inputs (files and directories) into Items in a stable
// order: inputs in the order given, directory contents in lexical order.
// It reads only each file's header; nothing is converted or created.
//
// In directories, hidden entries (names starting with ".") are skipped, as
// are FmtShift's own temporary files; symlinked files are followed and
// symlinked directories are not, so traversal cannot loop; and the output
// directory is not descended into if it lies inside an input.
//
// Inputs are opened without blocking, so a FIFO cannot stall discovery.
// If ctx is cancelled, discovery stops promptly and returns the items found
// so far together with ctx.Err(); callers should treat that as an
// interruption rather than run or summarise a partial batch.
func Discover(ctx context.Context, inputs []string, opt Options) ([]*Item, error) {
	var items []*Item
	var outInfo fs.FileInfo
	if opt.Template.OutputDir != "" {
		outInfo, _ = os.Stat(opt.Template.OutputDir)
	}

	for _, in := range inputs {
		if err := ctx.Err(); err != nil {
			return items, err
		}
		fi, err := os.Stat(in)
		if err != nil || !fi.IsDir() {
			// A file, or a path the engine will report as missing.
			it := newItem(in, true, opt.Template, opt.Template.OutputDir)
			if err == nil && len(opt.From) > 0 {
				if f, derr := filesystem.DetectFormat(in); derr == nil && !slices.Contains(opt.From, f) {
					it.Outcome = SkippedFiltered
				}
			}
			items = append(items, it)
			continue
		}
		items = append(items, discoverDir(ctx, in, opt, outInfo)...)
	}
	return items, ctx.Err()
}

func discoverDir(ctx context.Context, root string, opt Options, outInfo fs.FileInfo) []*Item {
	var items []*Item
	target := opt.Template.Format

	walk := func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return fs.SkipAll
		}
		if err != nil {
			it := newItem(path, path == root, opt.Template, "")
			// Classified like the engine's errors, so the CLI can report
			// "permission denied" or "not found" rather than a generic failure.
			it.Outcome, it.Err = Failed, converter.FSError(err)
			items = append(items, it)
			if d != nil && d.IsDir() && path != root {
				return fs.SkipDir
			}
			return nil
		}
		if path != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			if !opt.Recursive {
				return fs.SkipDir
			}
			if outInfo != nil {
				if di, err := os.Stat(path); err == nil && os.SameFile(di, outInfo) {
					return fs.SkipDir
				}
			}
			return nil
		}
		// Follow symlinks to regular files only; skip devices, pipes, etc.
		if d.Type()&fs.ModeSymlink != 0 {
			if fi, err := os.Stat(path); err != nil || !fi.Mode().IsRegular() {
				return nil
			}
		} else if !d.Type().IsRegular() {
			return nil
		}

		it := newItem(path, false, opt.Template, outputDirFor(root, path, opt))
		if testHookVisit != nil {
			testHookVisit()
		}
		f, err := filesystem.DetectFormat(path)
		switch {
		case errors.Is(err, format.ErrUnknown), errors.Is(err, format.ErrHeaderTooLarge),
			errors.Is(err, filesystem.ErrNotRegular):
			it.Outcome = SkippedUnsupported
		case err != nil:
			it.Outcome, it.Err = Failed, converter.FSError(err)
		case len(opt.From) > 0 && !slices.Contains(opt.From, f):
			it.Outcome = SkippedFiltered
		case f == target:
			it.Outcome = SkippedSameFormat
		}
		items = append(items, it)
		return nil
	}
	filepath.WalkDir(root, walk)
	return items
}

// outputDirFor returns the output directory for a file found under root.
// testHookVisit, if set by tests, runs for each file discovery classifies.
var testHookVisit func()

func outputDirFor(root, path string, opt Options) string {
	base := opt.Template.OutputDir
	if base == "" || !opt.Recursive {
		return base // "" means next to the source
	}
	rel, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil || rel == "." {
		return base
	}
	return filepath.Join(base, rel)
}

func newItem(src string, explicit bool, tmpl converter.Request, outDir string) *Item {
	req := tmpl
	req.SourcePath = src
	req.OutputDir = outDir
	if outDir != "" {
		req.CreateOutputDir = true
	}
	return &Item{Source: src, Explicit: explicit, Request: req}
}

// Run converts the Pending items with at most workers conversions at once
// and calls report once for every item, from the calling goroutine.
// Already-resolved items (skipped or failed during discovery) are reported
// first, in order; converted items are reported as they finish.
//
// Before converting anything, a preflight fails:
//   - inputs that would write the same output as an earlier input
//     (ErrDuplicate), with or without Overwrite;
//   - with Overwrite, inputs whose output would replace any other input of
//     the batch (ErrReplacesInput). Every discovered input is reserved,
//     including ones skipped as same-format, filtered or unsupported: an
//     original photo.jpg is indistinguishable from an earlier output, so it
//     is never replaced;
//   - inputs whose output would be created at the path of another input
//     that did not exist at preflight (ErrCreatesInput), with or without
//     Overwrite, so a missing input cannot appear mid-batch and hide its
//     error, and outcomes never depend on worker scheduling.
//
// Without Overwrite, an output path that already existed at preflight and
// is also an input is left to the engine, which reports SkippedExists:
// nothing is replaced, and re-runs stay safe.
//
// When ctx is cancelled, no new conversions start and the remaining items
// are reported as Cancelled.
func Run(ctx context.Context, cv *converter.Converter, items []*Item, workers int, report func(*Item)) {
	if workers < 1 {
		workers = 1
	}
	preflight(ctx, cv, items)

	var todo []*Item
	for _, it := range items {
		if it.Outcome == Pending {
			todo = append(todo, it)
		} else {
			report(it)
		}
	}

	jobs := make(chan *Item)
	done := make(chan *Item)
	var wg sync.WaitGroup
	for range min(workers, len(todo)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range jobs {
				convert(ctx, cv, it)
				done <- it
			}
		}()
	}
	go func() {
		for _, it := range todo {
			jobs <- it
		}
		close(jobs)
		wg.Wait()
		close(done)
	}()
	for it := range done {
		report(it)
	}
}

func convert(ctx context.Context, cv *converter.Converter, it *Item) {
	if ctx.Err() != nil {
		it.Outcome, it.Err = Cancelled, ctx.Err()
		return
	}
	res, err := cv.Convert(ctx, it.Request)
	it.Result, it.Err = res, err
	switch {
	case err == nil:
		it.Outcome = Converted
	case errors.Is(err, converter.ErrCancelled):
		it.Outcome = Cancelled
	case errors.Is(err, converter.ErrSameFormat):
		it.Outcome = SkippedSameFormat
	case errors.Is(err, converter.ErrOutputExists):
		it.Outcome = SkippedExists
	default:
		it.Outcome = Failed
	}
}

// Summary counts outcomes.
type Summary map[Outcome]int

// Summarize counts the outcomes of items.
func Summarize(items []*Item) Summary {
	s := Summary{}
	for _, it := range items {
		s[it.Outcome]++
	}
	return s
}

// Skipped is the total of all skipped outcomes.
func (s Summary) Skipped() int {
	return s[SkippedExists] + s[SkippedSameFormat] + s[SkippedUnsupported] + s[SkippedFiltered]
}

// Productive reports whether any input was eligible for conversion: it was
// converted, or would have been but its output already exists.
func (s Summary) Productive() bool {
	return s[Converted]+s[SkippedExists] > 0
}

// preflight fails pending items that conflict with other items; see Run.
func preflight(ctx context.Context, cv *converter.Converter, items []*Item) {
	var pending []*Item
	var reqs []converter.Request
	for _, it := range items {
		if it.Outcome == Pending {
			pending = append(pending, it)
			reqs = append(reqs, it.Request)
		}
	}
	fail := func(it *Item, kind error, other string) {
		dst, _ := cv.Destination(it.Request)
		it.Outcome, it.ConflictsWith = Failed, other
		it.Result.OutputPath = dst
		it.Err = fmt.Errorf("%w: %s", kind, other)
	}

	for i, j := range cv.DuplicateDestinations(ctx, reqs) {
		fail(pending[i], ErrDuplicate, pending[j].Source)
	}

	// Reserve every input, whatever its outcome, under two keys: its own
	// directory entry, and the file it actually reads, with all symlinks
	// resolved. Otherwise an input b/original.jpg -> a/photo.jpg would not
	// protect a/photo.jpg, and replacing that entry would change what the
	// input reads. Destinations keep using PathKey (final component not
	// resolved), because publishing replaces the destination's own entry.
	inputs := make(map[string]string, 2*len(items))
	for _, it := range items {
		inputs[converter.PathKey(it.Source)] = it.Source
		if target, err := filepath.EvalSymlinks(it.Source); err == nil {
			inputs[converter.PathKey(target)] = it.Source
		}
	}
	for _, it := range pending {
		if ctx.Err() != nil {
			return
		}
		if it.Outcome != Pending {
			continue
		}
		dst, ok := cv.Destination(it.Request)
		if !ok {
			continue
		}
		other, hit := inputs[converter.PathKey(dst)]
		// An output aliasing its own source is the engine's
		// ErrSourceIsOutput, reported with its own message.
		if !hit || converter.PathKey(other) == converter.PathKey(it.Source) {
			continue
		}
		_, statErr := os.Lstat(dst)
		existed := statErr == nil
		switch {
		case !existed:
			// Writing it would create another input's path, so the outcome
			// of that input would depend on scheduling.
			fail(it, ErrCreatesInput, other)
		case it.Request.Overwrite:
			fail(it, ErrReplacesInput, other)
		}
		// Otherwise the destination existed before the batch and Overwrite
		// is off: the engine reports SkippedExists, keeping re-runs safe.
	}
}
