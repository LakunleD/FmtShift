// Package format defines the image formats FmtShift understands and detects
// them from file content.
package format

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Format identifies an image file format.
type Format string

const (
	HEIC Format = "heic"
	JPEG Format = "jpeg"
	PNG  Format = "png"
	WebP Format = "webp"
)

// All lists every known format in display order.
var All = []Format{HEIC, JPEG, PNG, WebP}

// ErrUnknown is returned when content or a name does not match a supported format.
var ErrUnknown = errors.New("unknown image format")

// Parse converts a user-supplied format name, such as "jpg" or "HEIF", to a Format.
func Parse(name string) (Format, error) {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), ".")) {
	case "heic", "heif":
		return HEIC, nil
	case "jpeg", "jpg":
		return JPEG, nil
	case "png":
		return PNG, nil
	case "webp":
		return WebP, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknown, name)
}

// Extension returns the file extension used for outputs in this format.
func (f Format) Extension() string {
	switch f {
	case JPEG:
		return ".jpg"
	case HEIC, PNG, WebP:
		return "." + string(f)
	}
	return ""
}

// String returns the upper-case display name.
func (f Format) String() string { return strings.ToUpper(string(f)) }

// HeaderSize is the number of leading bytes Detect needs to identify any
// supported format, including HEIF files with long ftyp brand lists.
const HeaderSize = 256

var (
	jpegMagic = []byte{0xFF, 0xD8, 0xFF}
	pngMagic  = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}
)

// heicBrands are ftyp brands that indicate HEVC-coded HEIF content.
var heicBrands = map[string]bool{
	"heic": true, "heix": true, "heim": true, "heis": true,
	"hevc": true, "hevx": true, "hevm": true, "hevs": true,
}

// Detect identifies the format of an image from its leading bytes. It never
// relies on file extensions.
func Detect(header []byte) (Format, error) {
	switch {
	case bytes.HasPrefix(header, jpegMagic):
		return JPEG, nil
	case bytes.HasPrefix(header, pngMagic):
		return PNG, nil
	case len(header) >= 12 && string(header[0:4]) == "RIFF" && string(header[8:12]) == "WEBP":
		return WebP, nil
	case isHEIC(header):
		return HEIC, nil
	}
	return "", ErrUnknown
}

// isHEIC reports whether header begins with an ISO-BMFF ftyp box whose major
// or compatible brands identify HEVC-coded HEIF. Generic HEIF brands such as
// mif1 are also used by AVIF, so they alone are not sufficient.
func isHEIC(header []byte) bool {
	if len(header) < 16 || string(header[4:8]) != "ftyp" {
		return false
	}
	size := int(binary.BigEndian.Uint32(header[0:4]))
	if size < 16 || size > len(header) {
		size = len(header)
	}
	if heicBrands[string(header[8:12])] {
		return true
	}
	// header[12:16] is the minor version; compatible brands follow.
	for i := 16; i+4 <= size; i += 4 {
		if heicBrands[string(header[i:i+4])] {
			return true
		}
	}
	return false
}

// DetectFile reads the start of the file at path and identifies its format.
func DetectFile(path string) (Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	buf := make([]byte, HeaderSize)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", err
	}
	return Detect(buf[:n])
}
