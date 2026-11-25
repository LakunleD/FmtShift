// Package vips implements codecs.Codec using libvips, with HEIC decoding
// provided by libvips' libheif module.
//
// Building this package requires cgo and the libvips development files
// (found through pkg-config). libvips 8.15 or later is required.
package vips

/*
#cgo pkg-config: vips
#include <stdlib.h>
#include "fmtshift_vips.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/format"
)

const minMajor, minMinor = 8, 15

var (
	initOnce sync.Once
	initErr  error
)

func startup() error {
	initOnce.Do(func() {
		if C.fs_init() != 0 {
			initErr = fmt.Errorf("vips: initialisation failed: %s", takeError())
			return
		}
		major, minor := int(C.vips_version(0)), int(C.vips_version(1))
		if major < minMajor || (major == minMajor && minor < minMinor) {
			initErr = fmt.Errorf("vips: libvips %d.%d found, %d.%d or later required",
				major, minor, minMajor, minMinor)
		}
	})
	return initErr
}

// Codec converts images with libvips. It is safe for concurrent use.
type Codec struct {
	caps []codecs.Capability
}

var _ codecs.Codec = (*Codec)(nil)

// New initialises libvips (once per process) and returns a codec.
func New() (*Codec, error) {
	if err := startup(); err != nil {
		return nil, err
	}
	// HEIF support is a dynamically loaded libvips module and may be absent.
	caps := []codecs.Capability{
		{Format: format.HEIC, Read: hasOperation("heifload")},
		{Format: format.JPEG, Read: hasOperation("jpegload"), Write: hasOperation("jpegsave")},
		{Format: format.PNG, Read: hasOperation("pngload"), Write: hasOperation("pngsave")},
		{Format: format.WebP, Read: hasOperation("webpload"), Write: hasOperation("webpsave")},
	}
	return &Codec{caps: caps}, nil
}

// Name implements codecs.Codec.
func (c *Codec) Name() string {
	return "libvips " + C.GoString(C.vips_version_string())
}

// Capabilities implements codecs.Codec.
func (c *Codec) Capabilities() []codecs.Capability {
	return append([]codecs.Capability(nil), c.caps...)
}

// Convert implements codecs.Codec.
//
// libvips offers no cheap way to interrupt a running save, so ctx is only
// checked before work starts.
func (c *Codec) Convert(ctx context.Context, src string, srcFormat format.Format, dst string, dstFormat format.Format, opts codecs.Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !codecs.CanRead(c.caps, srcFormat) {
		return fmt.Errorf("%w: cannot read %s", codecs.ErrUnsupported, srcFormat)
	}
	if !codecs.CanWrite(c.caps, dstFormat) {
		return fmt.Errorf("%w: cannot write %s", codecs.ErrUnsupported, dstFormat)
	}
	if opts.Quality < 1 || opts.Quality > 100 {
		return fmt.Errorf("vips: quality %d out of range 1–100", opts.Quality)
	}
	maxPixels := opts.MaxPixels
	if maxPixels <= 0 {
		maxPixels = codecs.DefaultMaxPixels
	}

	cSrc, cDst := C.CString(src), C.CString(dst)
	defer C.free(unsafe.Pointer(cSrc))
	defer C.free(unsafe.Pointer(cDst))

	strip := C.int(0)
	if opts.StripMetadata {
		strip = 1
	}

	// libvips keeps per-thread buffers; pin the goroutine so they can be
	// released from the same OS thread afterwards.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer C.vips_thread_shutdown()

	rc := C.fs_convert(cSrc, formatCode(srcFormat), cDst, formatCode(dstFormat),
		C.int(opts.Quality), strip, C.gint64(maxPixels))
	msg := takeError()

	switch rc {
	case C.FS_OK:
		return nil
	case C.FS_ERR_LOAD:
		return fmt.Errorf("%w: %s", codecs.ErrDecode, msg)
	case C.FS_ERR_MULTI:
		return codecs.ErrMultipleImages
	case C.FS_ERR_TOO_LARGE:
		return fmt.Errorf("%w (%d pixels)", codecs.ErrTooLarge, maxPixels)
	case C.FS_ERR_FORMAT:
		return codecs.ErrUnsupported
	default:
		return fmt.Errorf("%w: %s", codecs.ErrConversion, msg)
	}
}

func formatCode(f format.Format) C.int {
	switch f {
	case format.HEIC:
		return C.FS_HEIC
	case format.JPEG:
		return C.FS_JPEG
	case format.PNG:
		return C.FS_PNG
	case format.WebP:
		return C.FS_WEBP
	}
	return 0
}

func hasOperation(name string) bool {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	return C.fs_has_operation(cName) != 0
}

// takeError returns and clears the libvips error buffer. The buffer is
// process-wide, so messages from concurrent conversions can interleave.
func takeError() string {
	p := C.vips_error_buffer_copy()
	if p == nil {
		return ""
	}
	defer C.g_free(C.gpointer(p))
	return strings.TrimSpace(C.GoString(p))
}

// info describes an image file; used by tests to inspect outputs.
type info struct {
	Width, Height int
	Pages         int
	Orientation   int
	Bands         int
	HasAlpha      bool
	HasICC        bool
	HasEXIF       bool
}

func probe(path string) (info, error) {
	if err := startup(); err != nil {
		return info{}, err
	}
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer C.vips_thread_shutdown()

	var ci C.fs_info
	if C.fs_probe(cPath, &ci) != 0 {
		return info{}, errors.New(takeError())
	}
	return info{
		Width:       int(ci.width),
		Height:      int(ci.height),
		Pages:       int(ci.pages),
		Orientation: int(ci.orientation),
		Bands:       int(ci.bands),
		HasAlpha:    ci.has_alpha != 0,
		HasICC:      ci.has_icc != 0,
		HasEXIF:     ci.has_exif != 0,
	}, nil
}
