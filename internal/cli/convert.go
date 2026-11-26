package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/lakunled/fmtshift/internal/batch"
	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/config"
	"github.com/lakunled/fmtshift/internal/converter"
	"github.com/lakunled/fmtshift/internal/format"
)

// Built-in defaults live in the config package, the single source for
// flag defaults, saved settings and their precedence.
const (
	DefaultWorkers = config.DefaultWorkers
	maxWorkers     = config.MaxWorkers // each conversion can use hundreds of MB
)

type convertFlags struct {
	to            string
	from          []string
	output        string
	quality       int
	recursive     bool
	overwrite     bool
	stripMetadata bool
	workers       int
}

func newConvertCmd(e *env) *cobra.Command {
	var f convertFlags
	cmd := &cobra.Command{
		Use:   "convert <file or folder>...",
		Short: "Convert image files and folders",
		Long: "Convert image files, or every supported image in a folder, to another\n" +
			"format.\n\n" +
			"Each output is written next to its source (or into --output) with the\n" +
			"same name and the new extension. Existing files are never replaced\n" +
			"unless --overwrite is given, and sources are never modified.\n\n" +
			"In folders, hidden files and files that are not supported images are\n" +
			"skipped; use --verbose to list them.",
		Example: "  fmtshift convert photo.heic\n" +
			"  fmtshift convert photo.heic --to png --output ./converted\n" +
			"  fmtshift convert ~/Pictures/Trip --from heic --to jpeg\n" +
			"  fmtshift convert ./photos --recursive --output ./jpegs --workers 4",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConvert(cmd, e, f, args)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&f.to, "to", "t", "jpeg", "output format: jpeg, png or webp")
	fl.StringSliceVar(&f.from, "from", nil, "only convert these input formats, e.g. heic or heic,png")
	fl.StringVarP(&f.output, "output", "o", "", "output directory (default: next to each source)")
	fl.IntVarP(&f.quality, "quality", "q", converter.DefaultQuality, "JPEG/WebP quality, 1-100")
	fl.BoolVarP(&f.recursive, "recursive", "r", false, "include subfolders, recreating them under --output")
	fl.BoolVar(&f.overwrite, "overwrite", false, "replace existing output files")
	fl.BoolVar(&f.stripMetadata, "strip-metadata", false, "remove EXIF, location and other metadata")
	fl.IntVarP(&f.workers, "workers", "w", DefaultWorkers, "number of images to convert at once")
	return cmd
}

func runConvert(cmd *cobra.Command, e *env, f convertFlags, args []string) error {
	// Validate the options typed on the command line before reading saved
	// settings, so an invalid value is a usage error (exit 2) even when the
	// configuration file is broken.
	typed := cmd.Flags().Changed
	if _, _, err := validateFlags(f, typed); err != nil {
		return err
	}

	// Precedence: typed options, then saved settings, then built-in
	// defaults. A broken configuration file stops the command (exit 3)
	// rather than silently falling back to defaults.
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	applyConfig(cmd, &f, cfg.Effective())

	// Then validate the effective values, whatever their source.
	target, from, err := validateFlags(f, func(string) bool { return true })
	if err != nil {
		return err
	}

	a, err := e.newApp()
	if err != nil {
		return &exitError{code: ExitFailed, err: fmt.Errorf("cannot start the image library: %w", err)}
	}
	if !codecs.CanWrite(a.Codec.Capabilities(), target) {
		return usageError("this build cannot write %s (run 'fmtshift formats')", target)
	}

	ctx := cmd.Context()
	items, err := batch.Discover(ctx, args, batch.Options{
		Recursive: f.recursive,
		From:      from,
		Template: converter.Request{
			Format:        target,
			OutputDir:     f.output,
			Quality:       f.quality,
			Overwrite:     f.overwrite,
			StripMetadata: f.stripMetadata,
		},
	})
	if err != nil {
		// Interrupted while scanning: nothing has been converted, and a
		// partial scan must not be summarised as "nothing to convert".
		fmt.Fprintln(e.stderr, "Interrupted.")
		return &exitError{code: ExitInterrupted}
	}

	start := time.Now()
	batch.Run(ctx, a.Converter, items, f.workers, func(it *batch.Item) { report(e, it) })
	sum := batch.Summarize(items)

	if len(items) > 1 || hasDirectory(args) {
		e.infof("\nConversion complete\n\n")
		e.infof("Converted: %d\n", sum[batch.Converted])
		e.infof("Skipped:   %d\n", sum.Skipped())
		e.infof("Failed:    %d\n", sum[batch.Failed])
		if sum[batch.Cancelled] > 0 {
			e.infof("Cancelled: %d\n", sum[batch.Cancelled])
		}
		e.infof("Duration:  %s\n", formatDuration(time.Since(start)))
	}

	// Interruption takes precedence over every other outcome: if the user
	// pressed Ctrl+C, report that, even if every item happened to finish or
	// be skipped.
	switch {
	case sum[batch.Cancelled] > 0, ctx.Err() != nil:
		fmt.Fprintln(e.stderr, "Interrupted.")
		return &exitError{code: ExitInterrupted}
	case sum[batch.Failed] > 0:
		return &exitError{code: ExitFailed}
	case !sum.Productive():
		fmt.Fprintf(e.stderr, "Nothing to convert: no inputs are supported images that need converting to %s.\n", target)
		return &exitError{code: ExitNoEligible}
	}
	return nil
}

// report prints one item's outcome. Skips of files found in folders are
// expected noise and only shown with --verbose; skips of files the user
// named are always shown.
func report(e *env, it *batch.Item) {
	src := it.Source
	switch it.Outcome {
	case batch.Converted:
		if e.verbose {
			e.infof("Converted %s -> %s (%s)\n", src, it.Result.OutputPath, formatDuration(it.Result.Duration))
		} else {
			e.infof("Converted %s -> %s\n", src, it.Result.OutputPath)
		}
	case batch.SkippedExists:
		e.infof("Skipped %s: %s already exists (use --overwrite to replace it)\n", src, it.Result.OutputPath)
	case batch.SkippedSameFormat:
		if it.Explicit || e.verbose {
			e.infof("Skipped %s: already %s\n", src, it.Request.Format)
		}
	case batch.SkippedFiltered:
		if it.Explicit || e.verbose {
			e.infof("Skipped %s: excluded by --from\n", src)
		}
	case batch.SkippedUnsupported:
		if e.verbose {
			e.infof("Skipped %s: not a supported image\n", src)
		}
	case batch.Failed:
		reportFailure(e, it)
	}
}

func reportFailure(e *env, it *batch.Item) {
	switch {
	case errors.Is(it.Err, batch.ErrReplacesInput):
		if it.Result.OutputPath == it.ConflictsWith {
			fmt.Fprintf(e.stderr, "Error: %s: would replace %s, which is one of the inputs; use --output to write somewhere else\n",
				it.Source, it.ConflictsWith)
		} else { // reached through a symlink, or differing only in case
			fmt.Fprintf(e.stderr, "Error: %s: would replace %s, which input %s refers to; use --output to write somewhere else\n",
				it.Source, it.Result.OutputPath, it.ConflictsWith)
		}
		return
	case errors.Is(it.Err, batch.ErrCreatesInput):
		fmt.Fprintf(e.stderr, "Error: %s: its output %s would take the place of input %s, which does not exist; convert them separately or use --output\n",
			it.Source, it.Result.OutputPath, it.ConflictsWith)
		return
	case errors.Is(it.Err, batch.ErrDuplicate):
		fmt.Fprintf(e.stderr, "Error: %s: would write the same output as %s (%s); convert them separately or into different --output directories\n",
			it.Source, it.ConflictsWith, it.Result.OutputPath)
		return
	}
	fmt.Fprintf(e.stderr, "Error: %s: %s\n", it.Source, describe(it.Err, it.Result))
	if e.verbose {
		fmt.Fprintf(e.stderr, "  detail: %v\n", it.Err)
	}
}

func hasDirectory(paths []string) bool {
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(100 * time.Millisecond).String()
}

// applyConfig fills every flag the user did not type with the saved or
// built-in default.
func applyConfig(cmd *cobra.Command, f *convertFlags, eff config.Effective) {
	fl := cmd.Flags()
	if !fl.Changed("to") {
		f.to = string(eff.Format)
	}
	if !fl.Changed("quality") {
		f.quality = eff.Quality
	}
	if !fl.Changed("workers") {
		f.workers = eff.Workers
	}
	if !fl.Changed("output") {
		f.output = eff.Output
	}
	if !fl.Changed("strip-metadata") {
		f.stripMetadata = eff.StripMetadata
	}
}

// validateFlags checks the convert options for which check(name) is true
// and returns the parsed output and --from formats.
func validateFlags(f convertFlags, check func(name string) bool) (format.Format, []format.Format, error) {
	var target format.Format
	if check("to") {
		t, err := format.Parse(f.to)
		if err != nil {
			return "", nil, usageError("unknown output format %q (use jpeg, png or webp)", f.to)
		}
		if t == format.HEIC {
			return "", nil, usageError("FmtShift cannot write HEIC (use jpeg, png or webp)")
		}
		target = t
	}
	var from []format.Format
	if check("from") {
		for _, name := range f.from {
			ff, err := format.Parse(name)
			if err != nil {
				return "", nil, usageError("unknown --from format %q (use heic, jpeg, png or webp)", name)
			}
			from = append(from, ff)
		}
	}
	if check("quality") && (f.quality < 1 || f.quality > 100) {
		return "", nil, usageError("--quality must be between 1 and 100, got %d", f.quality)
	}
	if check("workers") && (f.workers < 1 || f.workers > maxWorkers) {
		return "", nil, usageError("--workers must be between 1 and %d, got %d", maxWorkers, f.workers)
	}
	return target, from, nil
}
