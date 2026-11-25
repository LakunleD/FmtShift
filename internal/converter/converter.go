// Package converter is the conversion engine shared by the CLI and the
// desktop application. It validates requests, resolves output paths, guards
// the source file and publishes outputs safely; pixel work is delegated to a
// codecs.Codec.
package converter

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/filesystem"
	"github.com/lakunled/fmtshift/internal/format"
)

// DefaultQuality is used when Request.Quality is zero.
const DefaultQuality = 85

// Request describes one conversion. The zero value of every optional field
// is the safe default: keep metadata, never overwrite, never create
// directories.
type Request struct {
	// SourcePath is the file to convert. It is never modified.
	SourcePath string
	// Format is the output format.
	Format format.Format
	// OutputDir is the directory for the output; empty means the source's
	// directory. The output name is the source name with the new extension.
	OutputDir string
	// CreateOutputDir creates OutputDir (and parents) if it does not exist.
	CreateOutputDir bool
	// Quality is the lossy quality, 1–100; zero means DefaultQuality.
	Quality int
	// Overwrite replaces an existing output file. The source is never
	// replaced, even with Overwrite.
	Overwrite bool
	// StripMetadata removes EXIF, XMP, IPTC and ICC data from the output.
	StripMetadata bool
	// MaxPixels limits decoded image size; zero means the codec default.
	MaxPixels int64
}

// Result describes a completed conversion. On error, the fields known at the
// point of failure are filled in.
type Result struct {
	SourcePath   string
	OutputPath   string
	SourceFormat format.Format
	OutputFormat format.Format
	SourceSize   int64
	OutputSize   int64
	Duration     time.Duration
}

// Converter converts single files. It is safe for concurrent use if its
// codec is.
type Converter struct {
	codec codecs.Codec
	caps  []codecs.Capability
}

// New returns a Converter that uses codec for decoding and encoding.
func New(codec codecs.Codec) *Converter {
	return &Converter{codec: codec, caps: codec.Capabilities()}
}

// Convert converts req.SourcePath to req.Format.
//
// The source is opened exactly once. Its identity, size and format, the
// check that the output is not the source, and the codec's decoding all use
// that one handle, so replacing the path (or retargeting a symlink) during a
// conversion cannot make the checks describe one file and the output come
// from another.
//
// The output is written to a temporary file next to its destination and
// published only after the codec succeeds, so a failed or cancelled
// conversion never leaves a partial output. Without Overwrite, publication
// fails atomically if the destination exists, even if it appears during the
// conversion.
//
// The returned Result always carries Duration and whatever else was known
// when the conversion stopped, including on error.
func (c *Converter) Convert(ctx context.Context, req Request) (res Result, err error) {
	start := time.Now()
	res = Result{SourcePath: req.SourcePath, OutputFormat: req.Format}
	defer func() { res.Duration = time.Since(start) }()

	if err := ctx.Err(); err != nil {
		return res, cancelled(err)
	}
	quality, err := c.validate(req)
	if err != nil {
		return res, err
	}

	src, err := openSource(req.SourcePath)
	if err != nil {
		return res, fsError(err)
	}
	defer src.Close()

	srcInfo, err := src.Stat()
	if err != nil {
		return res, fsError(err)
	}
	if !srcInfo.Mode().IsRegular() {
		return res, fmt.Errorf("%w: %s is not a regular file", ErrUnsupportedInput, req.SourcePath)
	}
	res.SourceSize = srcInfo.Size()

	srcFormat, err := format.DetectReaderAt(src)
	switch {
	case errors.Is(err, format.ErrUnknown), errors.Is(err, format.ErrHeaderTooLarge):
		return res, fmt.Errorf("%w: %s: %w", ErrUnsupportedInput, req.SourcePath, err)
	case err != nil:
		return res, fsError(err)
	}
	res.SourceFormat = srcFormat
	if !codecs.CanRead(c.caps, srcFormat) {
		return res, fmt.Errorf("%w: this build cannot read %s", ErrUnsupportedInput, srcFormat)
	}
	if srcFormat == req.Format {
		return res, fmt.Errorf("%w: %s is already %s", ErrSameFormat, req.SourcePath, srcFormat)
	}

	dst, err := outputPath(req)
	if err != nil {
		return res, err
	}
	res.OutputPath = dst

	if err := checkNotSource(srcInfo, dst); err != nil {
		return res, err
	}
	if !req.Overwrite {
		if _, err := os.Lstat(dst); err == nil {
			return res, fmt.Errorf("%w: %s", ErrOutputExists, dst)
		}
	}

	opts := codecs.Options{Quality: quality, StripMetadata: req.StripMetadata, MaxPixels: req.MaxPixels}
	write := func(tmp string) error {
		if err := c.codec.Convert(ctx, src, srcFormat, tmp, req.Format, opts); err != nil {
			return codecError(err)
		}
		// Do not publish an output the caller no longer wants.
		if err := ctx.Err(); err != nil {
			return cancelled(err)
		}
		return nil
	}

	publish := filesystem.WriteNew
	if req.Overwrite {
		publish = filesystem.WriteReplace
	}
	if err := publish(dst, write); err != nil {
		switch {
		case errors.Is(err, filesystem.ErrExists):
			return res, fmt.Errorf("%w: %s", ErrOutputExists, dst)
		case isKind(err):
			return res, err
		default:
			return res, fsError(err)
		}
	}

	if fi, err := os.Stat(dst); err == nil {
		res.OutputSize = fi.Size()
	}
	return res, nil
}

// validate checks options that do not depend on the source and returns the
// effective quality.
func (c *Converter) validate(req Request) (int, error) {
	if req.SourcePath == "" {
		return 0, fmt.Errorf("%w: no source path", ErrInvalidOptions)
	}
	if !codecs.CanWrite(c.caps, req.Format) {
		return 0, fmt.Errorf("%w: cannot write %q", ErrInvalidOptions, req.Format)
	}
	quality := req.Quality
	if quality == 0 {
		quality = DefaultQuality
	}
	if quality < 1 || quality > 100 {
		return 0, fmt.Errorf("%w: quality %d out of range 1–100", ErrInvalidOptions, req.Quality)
	}
	if req.MaxPixels < 0 {
		return 0, fmt.Errorf("%w: negative pixel limit", ErrInvalidOptions)
	}
	return quality, nil
}

// outputPath resolves the destination file, creating the output directory if
// requested. The name is the source name with its last extension replaced:
// "IMG 1.HEIC" → "IMG 1.jpg", "archive.tar.heic" → "archive.tar.jpg".
func outputPath(req Request) (string, error) {
	dir := outputDir(req)
	fi, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if !req.CreateOutputDir {
			return "", fmt.Errorf("%w: output directory %s does not exist", ErrNotFound, dir)
		}
		if err := os.MkdirAll(dir, 0o777); err != nil {
			return "", fsError(err)
		}
	case err != nil:
		return "", fsError(err)
	case !fi.IsDir():
		return "", fmt.Errorf("%w: output %s is not a directory", ErrInvalidOptions, dir)
	}

	return filepath.Join(dir, outputName(req.SourcePath, req.Format)), nil
}

// outputDir is the directory req writes into.
func outputDir(req Request) string {
	if req.OutputDir != "" {
		return req.OutputDir
	}
	return filepath.Dir(req.SourcePath)
}

// outputName is the output file name for src in format f.
func outputName(src string, f format.Format) string {
	base := filepath.Base(src)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "" { // a dotfile such as ".heic"
		stem = base
	}
	return stem + f.Extension()
}

// checkNotSource rejects a destination that is the source file under another
// name: a symlink or hard link to it, a different case on a case-insensitive
// filesystem, or a misnamed source (e.g. HEIC content in "photo.jpg"
// converted to JPEG). os.SameFile compares the underlying files, so it
// catches all of these. src must come from the open source handle, so the
// comparison is against the file actually being converted.
//
// Publication replaces directory entries (link or rename), never file
// contents, so even if the destination changes after this check the
// source's data cannot be overwritten.
func checkNotSource(src fs.FileInfo, dst string) error {
	fi, err := os.Stat(dst)
	if err != nil {
		return nil // no file at dst, so it cannot be the source
	}
	if os.SameFile(src, fi) {
		return fmt.Errorf("%w: %s", ErrSourceIsOutput, dst)
	}
	return nil
}
