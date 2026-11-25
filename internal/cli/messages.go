package cli

import (
	"errors"

	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/converter"
	"github.com/lakunled/fmtshift/internal/format"
)

// describe turns a conversion error into a short, actionable message. The
// full error chain is shown with --verbose.
func describe(err error, res converter.Result) string {
	switch {
	// Specific causes first; they are wrapped inside the converter kinds.
	case errors.Is(err, codecs.ErrHDR):
		return "HDR images (PQ/HLG) are not supported yet"
	case errors.Is(err, codecs.ErrColourSpace):
		return "uses a colour space that cannot be converted faithfully (wide-gamut without an ICC profile)"
	case errors.Is(err, codecs.ErrMultipleImages):
		return "contains multiple images; animations, bursts and image sequences are not supported"
	case errors.Is(err, codecs.ErrTooLarge):
		return "image dimensions are too large"
	case errors.Is(err, format.ErrUnknown), errors.Is(err, format.ErrHeaderTooLarge):
		return "not a supported image (expected HEIC, JPEG, PNG or WebP)"

	case errors.Is(err, converter.ErrSourceIsOutput):
		return "the output " + res.OutputPath + " would replace the source file itself; use --output to choose another directory"
	case errors.Is(err, converter.ErrNotFound):
		return "no such file or directory"
	case errors.Is(err, converter.ErrPermission):
		return "permission denied"
	case errors.Is(err, converter.ErrCorrupt):
		return "the file is corrupt or not a valid image"
	case errors.Is(err, converter.ErrUnsupportedInput):
		return "not a supported image"
	case errors.Is(err, converter.ErrInvalidOptions):
		return err.Error()
	}
	return "conversion failed: " + err.Error()
}
