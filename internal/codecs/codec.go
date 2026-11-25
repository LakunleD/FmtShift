// Package codecs defines the boundary between the conversion engine and the
// image-processing libraries that decode and encode pixels.
package codecs

import (
	"context"
	"errors"

	"github.com/lakunled/fmtshift/internal/format"
)

// Capability describes what a codec can do with one format.
type Capability struct {
	Format format.Format
	Read   bool
	Write  bool
}

// Options control how an image is encoded.
type Options struct {
	// Quality is the lossy compression quality, 1–100. Ignored by lossless formats.
	Quality int
	// StripMetadata removes EXIF, XMP, IPTC and ICC data. Pixels are converted
	// to sRGB first so colours are preserved without the embedded profile.
	StripMetadata bool
	// MaxPixels rejects inputs whose width × height exceeds this value
	// before full decoding. Zero means DefaultMaxPixels.
	MaxPixels int64
}

// DefaultMaxPixels bounds decoded image size (about 16k × 16k).
const DefaultMaxPixels int64 = 268_402_689

// Codec converts a single still image from one format to another.
//
// Implementations produce physically upright pixels: container transforms
// and EXIF orientation are applied exactly once, and any orientation tag in
// preserved metadata is normalized. Transparent images written to formats
// without alpha are composited onto white.
type Codec interface {
	// Name identifies the implementation, e.g. "libvips 8.18.7".
	Name() string
	// Capabilities reports the formats supported by this build.
	Capabilities() []Capability
	// Convert decodes src, which the caller has identified as srcFormat, and
	// writes dstFormat to dst, replacing any file already at dst. Callers are
	// responsible for choosing a safe dst (for example a temporary file) and
	// for removing it if Convert fails.
	Convert(ctx context.Context, src string, srcFormat format.Format, dst string, dstFormat format.Format, opts Options) error
}

var (
	// ErrUnsupported means the codec cannot read or write the requested format.
	ErrUnsupported = errors.New("unsupported format")
	// ErrMultipleImages means the input holds more than one user-visible image
	// (animation, sequence or multi-image container).
	ErrMultipleImages = errors.New("input contains multiple images")
	// ErrTooLarge means the input exceeds Options.MaxPixels.
	ErrTooLarge = errors.New("image dimensions exceed limit")
	// ErrDecode means the input could not be opened as an image, usually
	// because it is corrupt or truncated.
	ErrDecode = errors.New("cannot decode image")
	// ErrConversion means processing or writing failed after the input was
	// opened. Decoding is lazy, so corrupt pixel data can also surface here.
	ErrConversion = errors.New("conversion failed")
)

// CanRead reports whether caps include reading f.
func CanRead(caps []Capability, f format.Format) bool {
	for _, c := range caps {
		if c.Format == f {
			return c.Read
		}
	}
	return false
}

// CanWrite reports whether caps include writing f.
func CanWrite(caps []Capability, f format.Format) bool {
	for _, c := range caps {
		if c.Format == f {
			return c.Write
		}
	}
	return false
}
