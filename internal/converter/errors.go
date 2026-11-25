package converter

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/lakunled/fmtshift/internal/codecs"
)

// Error kinds returned by Convert. Every error from Convert matches exactly
// one of these with errors.Is; the underlying cause (OS or codec error) is
// wrapped as well, so errors.Is(err, codecs.ErrHDR) also works.
var (
	// ErrInvalidOptions: the request itself is invalid (bad quality, an
	// output format this build cannot write, output path not a directory).
	ErrInvalidOptions = errors.New("invalid conversion options")
	// ErrNotFound: the source or output directory does not exist.
	ErrNotFound = errors.New("not found")
	// ErrPermission: the OS denied access to the source or output location.
	ErrPermission = errors.New("permission denied")
	// ErrUnsupportedInput: the source is not a supported image, or uses a
	// feature FmtShift does not handle (multiple images, HDR, too large).
	ErrUnsupportedInput = errors.New("unsupported input")
	// ErrCorrupt: the source looks like a supported format but cannot be read.
	ErrCorrupt = errors.New("corrupt or unreadable image")
	// ErrSameFormat: the source is already in the target format; skipped.
	ErrSameFormat = errors.New("already in target format")
	// ErrOutputExists: the destination exists and Overwrite is not set.
	ErrOutputExists = errors.New("output already exists")
	// ErrSourceIsOutput: the destination is the source file itself (by
	// name, link or case-insensitive match). Never allowed.
	ErrSourceIsOutput = errors.New("output would replace the source file")
	// ErrConversionFailed: decoding or encoding failed, or an unexpected
	// I/O error occurred.
	ErrConversionFailed = errors.New("conversion failed")
	// ErrCancelled: the context was cancelled; no output was published.
	ErrCancelled = errors.New("conversion cancelled")
)

var kinds = []error{
	ErrInvalidOptions, ErrNotFound, ErrPermission, ErrUnsupportedInput,
	ErrCorrupt, ErrSameFormat, ErrOutputExists, ErrSourceIsOutput,
	ErrConversionFailed, ErrCancelled,
}

// isKind reports whether err already carries one of the converter's kinds.
func isKind(err error) bool {
	for _, k := range kinds {
		if errors.Is(err, k) {
			return true
		}
	}
	return false
}

// fsError classifies an OS error.
func fsError(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%w: %w", ErrPermission, err)
	}
	return fmt.Errorf("%w: %w", ErrConversionFailed, err)
}

// codecError classifies an error from codecs.Codec.Convert. Source I/O
// causes are checked before codec kinds: an error that wraps both
// codecs.ErrDecode and fs.ErrPermission is an access problem, not a corrupt
// image.
func codecError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return cancelled(err)
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%w: %w", ErrPermission, err)
	case errors.Is(err, codecs.ErrDecode):
		return fmt.Errorf("%w: %w", ErrCorrupt, err)
	case errors.Is(err, codecs.ErrUnsupported),
		errors.Is(err, codecs.ErrMultipleImages),
		errors.Is(err, codecs.ErrTooLarge),
		errors.Is(err, codecs.ErrHDR),
		errors.Is(err, codecs.ErrColourSpace):
		return fmt.Errorf("%w: %w", ErrUnsupportedInput, err)
	}
	return fmt.Errorf("%w: %w", ErrConversionFailed, err)
}

func cancelled(err error) error {
	return fmt.Errorf("%w: %w", ErrCancelled, err)
}
