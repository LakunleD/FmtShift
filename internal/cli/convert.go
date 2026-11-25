package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/converter"
	"github.com/lakunled/fmtshift/internal/format"
)

type convertFlags struct {
	to            string
	output        string
	quality       int
	overwrite     bool
	stripMetadata bool
}

func newConvertCmd(e *env) *cobra.Command {
	var f convertFlags
	cmd := &cobra.Command{
		Use:   "convert <file>...",
		Short: "Convert image files",
		Long: "Convert one or more image files to another format.\n\n" +
			"Each output is written next to its source (or into --output) with the\n" +
			"same name and the new extension. Existing files are never replaced\n" +
			"unless --overwrite is given, and sources are never modified.",
		Example: "  fmtshift convert photo.heic\n" +
			"  fmtshift convert photo.heic --to png --output ./converted\n" +
			"  fmtshift convert IMG_0001.HEIC IMG_0002.HEIC --quality 90",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConvert(cmd, e, f, args)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&f.to, "to", "t", "jpeg", "output format: jpeg, png or webp")
	fl.StringVarP(&f.output, "output", "o", "", "output directory (default: next to each source)")
	fl.IntVarP(&f.quality, "quality", "q", converter.DefaultQuality, "JPEG/WebP quality, 1-100")
	fl.BoolVar(&f.overwrite, "overwrite", false, "replace existing output files")
	fl.BoolVar(&f.stripMetadata, "strip-metadata", false, "remove EXIF, location and other metadata")
	return cmd
}

// outcome is how one input ended up.
type outcome int

const (
	converted  outcome = iota
	skipped            // output exists; safe to re-run
	ineligible         // already in the target format
	failed
	interrupted
)

func runConvert(cmd *cobra.Command, e *env, f convertFlags, args []string) error {
	target, err := format.Parse(f.to)
	if err != nil {
		return usageError("unknown output format %q (use jpeg, png or webp)", f.to)
	}
	if f.quality < 1 || f.quality > 100 {
		return usageError("--quality must be between 1 and 100, got %d", f.quality)
	}

	a, err := e.newApp()
	if err != nil {
		return &exitError{code: ExitFailed, err: fmt.Errorf("cannot start the image library: %w", err)}
	}
	if !codecs.CanWrite(a.Codec.Capabilities(), target) {
		return usageError("this build cannot write %s (run 'fmtshift formats')", target)
	}

	reqs := make([]converter.Request, len(args))
	for i, src := range args {
		reqs[i] = converter.Request{
			SourcePath:      src,
			Format:          target,
			OutputDir:       f.output,
			CreateOutputDir: f.output != "", // the user named it, so create it
			Quality:         f.quality,
			Overwrite:       f.overwrite,
			StripMetadata:   f.stripMetadata,
		}
	}
	// Preflight before converting anything: inputs that would write the
	// same output as an earlier input fail, with or without --overwrite.
	dups := a.Converter.DuplicateDestinations(reqs)

	ctx := cmd.Context()
	start := time.Now()
	counts := map[outcome]int{}
	for i, req := range reqs {
		if ctx.Err() != nil {
			counts[interrupted]++
			continue
		}
		if j, dup := dups[i]; dup {
			dst, _ := a.Converter.Destination(req)
			fmt.Fprintf(e.stderr, "Error: %s: would write the same output as %s (%s); convert them separately or into different --output directories\n",
				req.SourcePath, reqs[j].SourcePath, dst)
			counts[failed]++
			continue
		}
		counts[convertOne(ctx, e, a.Converter, req)]++
	}

	if len(args) > 1 {
		e.infof("\nConversion complete\n\n")
		e.infof("Converted: %d\n", counts[converted])
		e.infof("Skipped:   %d\n", counts[skipped]+counts[ineligible])
		e.infof("Failed:    %d\n", counts[failed])
		if counts[interrupted] > 0 {
			e.infof("Cancelled: %d\n", counts[interrupted])
		}
		e.infof("Duration:  %s\n", formatDuration(time.Since(start)))
	}

	switch {
	case counts[interrupted] > 0:
		fmt.Fprintln(e.stderr, "Interrupted.")
		return &exitError{code: ExitInterrupted}
	case counts[failed] > 0:
		return &exitError{code: ExitFailed}
	case counts[converted] == 0 && counts[skipped] == 0:
		return &exitError{code: ExitNoEligible}
	}
	return nil
}

// convertOne converts a single input and reports the result.
func convertOne(ctx context.Context, e *env, cv *converter.Converter, req converter.Request) outcome {
	src := req.SourcePath
	if fi, err := os.Stat(src); err == nil && fi.IsDir() {
		fmt.Fprintf(e.stderr, "Error: %s: is a directory (directory conversion is not supported yet)\n", src)
		return failed
	}

	res, err := cv.Convert(ctx, req)
	switch {
	case err == nil:
		if e.verbose {
			e.infof("Converted %s -> %s (%s)\n", src, res.OutputPath, formatDuration(res.Duration))
		} else {
			e.infof("Converted %s -> %s\n", src, res.OutputPath)
		}
		return converted
	case errors.Is(err, converter.ErrCancelled):
		return interrupted
	case errors.Is(err, converter.ErrSameFormat):
		e.infof("Skipped %s: already %s\n", src, res.SourceFormat)
		return ineligible
	case errors.Is(err, converter.ErrOutputExists):
		e.infof("Skipped %s: %s already exists (use --overwrite to replace it)\n", src, res.OutputPath)
		return skipped
	}
	fmt.Fprintf(e.stderr, "Error: %s: %s\n", src, describe(err, res))
	if e.verbose {
		fmt.Fprintf(e.stderr, "  detail: %v\n", err)
	}
	return failed
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(100 * time.Millisecond).String()
}
