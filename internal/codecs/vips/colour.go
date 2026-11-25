package vips

import (
	"fmt"

	"github.com/lakunled/fmtshift/internal/codecs"
)

// ITU-T H.273 code points used in HEIF NCLX colour boxes.
const (
	primariesBT709       = 1
	primariesUnspecified = 2
	primariesBT601_625   = 5
	primariesBT601_525   = 6
	primariesDisplayP3   = 12 // SMPTE EG 432-1 (P3-D65)

	transferBT709       = 1
	transferUnspecified = 2
	transferBT601       = 6
	transferSRGB        = 13
	transferBT2020_10   = 14
	transferBT2020_12   = 15
	transferPQ          = 16 // SMPTE ST 2084 / BT.2100 PQ
	transferHLG         = 18 // ARIB STD-B67 / BT.2100 HLG

	noNCLX = -1
)

// heifColour is the colour signalling of a HEIF primary image.
type heifColour struct {
	HasICC    bool
	Primaries int // H.273 code point, or noNCLX
	Transfer  int // H.273 code point, or noNCLX
}

// sdrTransfers are transfer functions close enough to the sRGB curve to be
// handled as SDR. The BT.709/BT.2020 camera curves differ slightly from sRGB
// in the shadows; the difference is accepted for v0.1.
var sdrTransfers = map[int]bool{
	transferBT709: true, transferUnspecified: true, transferBT601: true,
	transferSRGB: true, transferBT2020_10: true, transferBT2020_12: true,
}

// sRGBPrimaries are primaries treated as sRGB. BT.601 primaries differ
// slightly from BT.709/sRGB; the difference is accepted for v0.1.
var sRGBPrimaries = map[int]bool{
	primariesBT709: true, primariesUnspecified: true,
	primariesBT601_625: true, primariesBT601_525: true,
}

// heifColourPolicy decides how to convert a HEIF image given its colour
// signalling. It returns the name of a libvips built-in ICC profile to attach
// (or "" for none), or an error if the image must be rejected.
//
// libvips does not read NCLX boxes, so without this step every NCLX image
// would be treated as sRGB: HDR images would be written as washed-out SDR
// and wide-gamut colours would be desaturated.
func heifColourPolicy(c heifColour) (string, error) {
	// HDR is rejected even when an ICC profile is present: converting PQ/HLG
	// to SDR needs tone mapping, which v0.1 does not implement.
	switch c.Transfer {
	case transferPQ:
		return "", fmt.Errorf("%w (PQ transfer function)", codecs.ErrHDR)
	case transferHLG:
		return "", fmt.Errorf("%w (HLG transfer function)", codecs.ErrHDR)
	}

	// An embedded ICC profile fully describes the pixels; libvips uses it.
	if c.HasICC || c.Primaries == noNCLX {
		return "", nil
	}

	if !sdrTransfers[c.Transfer] {
		return "", fmt.Errorf("%w: transfer characteristics %d without an ICC profile",
			codecs.ErrColourSpace, c.Transfer)
	}
	switch {
	case sRGBPrimaries[c.Primaries]:
		return "", nil
	case c.Primaries == primariesDisplayP3:
		return "p3", nil
	}
	return "", fmt.Errorf("%w: colour primaries %d without an ICC profile",
		codecs.ErrColourSpace, c.Primaries)
}
