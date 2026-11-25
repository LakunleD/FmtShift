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

// Lossy reports whether encoding to f uses a quality setting. WebP is treated
// as lossy because FmtShift always writes lossy WebP.
func (f Format) Lossy() bool { return f == JPEG || f == WebP }

// String returns the upper-case display name.
func (f Format) String() string { return strings.ToUpper(string(f)) }

const (
	// HeaderSize is the number of leading bytes DetectFile reads first. It
	// covers every format's signature and typical HEIF ftyp boxes.
	HeaderSize = 4096
	// MaxFtypSize bounds how much of a declared HEIF ftyp box DetectFile will
	// read. Real files use well under 100 bytes; the cap stops a malformed
	// size field from causing a large read.
	MaxFtypSize = 64 << 10
)

// ErrHeaderTooLarge is returned when a file declares an ftyp box larger than
// MaxFtypSize, so its brands cannot be checked.
var ErrHeaderTooLarge = errors.New("file type header too large")

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
//
// Box layout (ISO/IEC 14496-12 §4.2): a 32-bit size and "ftyp", then an
// optional 64-bit largesize when size == 1, then major brand, minor version
// and compatible brands up to the end of the box. Size 0 means the box
// extends to the end of the file.
func isHEIC(header []byte) bool {
	start, size, ok := ftypBox(header)
	if !ok {
		return false
	}
	if size == 0 {
		size = uint64(len(header))
	}
	// The box must hold at least the major brand and minor version.
	if size < uint64(start+8) {
		return false
	}
	end := len(header)
	if size < uint64(end) {
		end = int(size)
	}
	if end < start+4 {
		return false
	}

	if heicBrands[string(header[start:start+4])] {
		return true
	}
	// Skip major brand and minor version; compatible brands follow.
	for i := start + 8; i+4 <= end; i += 4 {
		if heicBrands[string(header[i:i+4])] {
			return true
		}
	}
	return false
}

// ftypBox parses the header of an ISO-BMFF ftyp box at the start of header.
// It returns the offset of the major brand and the declared box size, where
// size 0 means "to the end of the file".
func ftypBox(header []byte) (start int, size uint64, ok bool) {
	if len(header) < 8 || string(header[4:8]) != "ftyp" {
		return 0, 0, false
	}
	switch size32 := binary.BigEndian.Uint32(header[0:4]); size32 {
	case 0:
		return 8, 0, true
	case 1:
		if len(header) < 16 {
			return 0, 0, false
		}
		return 16, binary.BigEndian.Uint64(header[8:16]), true
	default:
		return 8, uint64(size32), true
	}
}

// DetectFile reads the start of the file at path and identifies its format.
// For HEIF files it reads the whole declared ftyp box, up to MaxFtypSize, so
// compatible brands are never missed because of a fixed read size.
func DetectFile(path string) (Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return DetectReaderAt(f)
}

// DetectReaderAt identifies the format of the content in r, reading from
// offset 0, with the same rules as DetectFile. Use it to check a file that is
// already open, so detection and decoding see the same file.
func DetectReaderAt(r io.ReaderAt) (Format, error) {
	f := io.NewSectionReader(r, 0, 1<<62)
	buf, err := readUpTo(f, HeaderSize)
	if err != nil {
		return "", err
	}

	if _, size, ok := ftypBox(buf); ok && (size == 0 || size > uint64(len(buf))) && len(buf) == HeaderSize {
		want := uint64(MaxFtypSize) // size 0: box runs to end of file
		if size != 0 {
			if size > MaxFtypSize {
				return "", fmt.Errorf("%w: declared ftyp box is %d bytes", ErrHeaderTooLarge, size)
			}
			want = size
		}
		rest, err := readUpTo(f, int(want)-len(buf))
		if err != nil {
			return "", err
		}
		buf = append(buf, rest...)
	}
	return Detect(buf)
}

// readUpTo reads at most n bytes from r, stopping early at end of file.
func readUpTo(r io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	got, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:got], nil
}
