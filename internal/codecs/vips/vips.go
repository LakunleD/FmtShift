// Package vips implements codecs.Codec using libvips, with HEIC decoding
// provided by libvips' libheif module. libheif is also linked directly to
// read HEIF colour signalling that libvips does not expose.
//
// Building this package requires cgo and the libvips and libheif development
// files (found through pkg-config). libvips 8.15 or later is required.
package vips

/*
#cgo pkg-config: vips libheif
#include <stdlib.h>
#include "fmtshift_vips.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// vipsErrMu makes libvips error messages attributable. libvips collects
// errors in a single process-wide buffer, so with concurrent conversions a
// failure's message could be read, cleared or mixed up by another
// conversion. Conversions hold the read lock, so they run concurrently and
// never touch the buffer. A failed conversion is retried once under the
// write lock, with no other libvips work running, and only that retry's
// message is reported.
var vipsErrMu sync.RWMutex

// Convert implements codecs.Codec.
//
// Everything reads the caller's open handle: the content check against
// srcFormat, HEIF colour inspection and decoding. The path is never
// reopened, so the caller's checks and this codec's work concern one file.
//
// libvips offers no cheap way to interrupt a running save, so ctx is only
// checked before work starts.
func (c *Codec) Convert(ctx context.Context, in *os.File, srcFormat format.Format, dst string, dstFormat format.Format, opts codecs.Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !codecs.CanRead(c.caps, srcFormat) {
		return fmt.Errorf("%w: cannot read %s", codecs.ErrUnsupported, srcFormat)
	}
	if !codecs.CanWrite(c.caps, dstFormat) {
		return fmt.Errorf("%w: cannot write %s", codecs.ErrUnsupported, dstFormat)
	}
	quality := opts.Quality
	if dstFormat.Lossy() {
		if quality < 1 || quality > 100 {
			return fmt.Errorf("vips: quality %d out of range 1–100", quality)
		}
	} else {
		quality = 0 // ignored by lossless savers
	}
	maxPixels := opts.MaxPixels
	if maxPixels <= 0 {
		maxPixels = codecs.DefaultMaxPixels
	}

	src := in.Name()
	// OS errors are wrapped with %w so callers can still see causes such as
	// fs.ErrPermission.
	if fi, err := in.Stat(); err != nil {
		return fmt.Errorf("vips: %w", err)
	} else if !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file", codecs.ErrDecode, src)
	}
	if got, err := format.DetectReaderAt(in); err != nil {
		if errors.Is(err, format.ErrUnknown) || errors.Is(err, format.ErrHeaderTooLarge) {
			return fmt.Errorf("%w: %s: %w", codecs.ErrDecode, src, err)
		}
		return fmt.Errorf("vips: %s: %w", src, err)
	} else if got != srcFormat {
		return fmt.Errorf("%w: %s contains %s, not %s", codecs.ErrDecode, src, got, srcFormat)
	}

	j := job{
		fd:        C.int(in.Fd()),
		srcFormat: srcFormat,
		dst:       C.CString(dst),
		dstFormat: formatCode(dstFormat),
		quality:   C.int(quality),
		maxPixels: C.gint64(maxPixels),
	}
	defer C.free(unsafe.Pointer(j.dst))
	if opts.StripMetadata {
		j.strip = 1
	}

	// libvips keeps per-thread buffers; pin the goroutine so they can be
	// released from the same OS thread afterwards.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer C.vips_thread_shutdown()

	vipsErrMu.RLock()
	rc, err := j.run()
	vipsErrMu.RUnlock()
	if err != nil {
		return err
	}

	var msg string
	if rc == C.FS_ERR_LOAD || rc == C.FS_ERR_PROCESS {
		vipsErrMu.Lock()
		C.vips_error_clear()
		rc, err = j.run()
		msg = takeError()
		vipsErrMu.Unlock()
		if err != nil {
			return err
		}
		if msg == "" {
			msg = "no details reported by libvips"
		}
	}
	runtime.KeepAlive(in)

	switch rc {
	case C.FS_OK:
		return nil // a failure that did not recur on retry
	case C.FS_ERR_LOAD:
		return fmt.Errorf("%w: %s: %s", codecs.ErrDecode, src, msg)
	case C.FS_ERR_MULTI:
		return codecs.ErrMultipleImages
	case C.FS_ERR_TOO_LARGE:
		return fmt.Errorf("%w (%d pixels)", codecs.ErrTooLarge, maxPixels)
	case C.FS_ERR_FORMAT:
		return codecs.ErrUnsupported
	default:
		return fmt.Errorf("%w: %s: %s", codecs.ErrConversion, src, msg)
	}
}

// job holds the C arguments for one conversion so it can be retried.
type job struct {
	fd        C.int
	srcFormat format.Format
	dst       *C.char
	dstFormat C.int
	quality   C.int
	strip     C.int
	maxPixels C.gint64
}

// run performs one conversion attempt. A non-nil error is a deterministic
// rejection (colour policy) that needs no libvips message; otherwise the
// fs_convert result code is returned. It never reads the error buffer.
func (j job) run() (C.int, error) {
	var assign *C.char
	if j.srcFormat == format.HEIC {
		var hc C.fs_heif_colour
		if C.fs_heif_colour_info(j.fd, &hc) != 0 {
			return C.FS_ERR_LOAD, nil
		}
		profile, err := heifColourPolicy(heifColour{
			HasICC:    hc.has_icc != 0,
			Primaries: int(hc.primaries),
			Transfer:  int(hc.transfer),
		})
		if err != nil {
			return 0, err
		}
		if profile != "" {
			assign = C.CString(profile)
			defer C.free(unsafe.Pointer(assign))
		}
	}
	if testHookBeforeDecode != nil {
		testHookBeforeDecode()
	}
	return C.fs_convert(j.fd, formatCode(j.srcFormat), j.dst, j.dstFormat,
		j.quality, j.strip, j.maxPixels, assign), nil
}

// testHookBeforeDecode, if set by tests, runs between inspection and
// decoding, to simulate the source path being replaced mid-conversion.
var testHookBeforeDecode func()

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

// takeError returns and clears the libvips error buffer.
//
// The buffer is process-wide and also collects messages from work that
// succeeded (warnings), so callers must hold vipsErrMu for writing (or be in
// one-time initialisation), call vips_error_clear before the operation whose
// error they want, and call takeError right after it.
//
// vips_error_buffer_copy already clears the buffer in libvips 8.15+; the
// explicit clear keeps that guarantee independent of the library version.
func takeError() string {
	p := C.vips_error_buffer_copy()
	C.vips_error_clear()
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

	vipsErrMu.Lock()
	defer vipsErrMu.Unlock()
	// Discard messages left by earlier work (e.g. warnings from successful
	// conversions) so a failure reports only its own message.
	C.vips_error_clear()

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
