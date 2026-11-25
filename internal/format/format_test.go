package format

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	tests := map[string]Format{
		"heic": HEIC, "HEIF": HEIC, "jpg": JPEG, "JPEG": JPEG, ".jpg": JPEG,
		" png ": PNG, "WebP": WebP,
	}
	for in, want := range tests {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "gif", "avif", "jp"} {
		if _, err := Parse(in); !errors.Is(err, ErrUnknown) {
			t.Errorf("Parse(%q) error = %v; want ErrUnknown", in, err)
		}
	}
}

func TestExtension(t *testing.T) {
	want := map[Format]string{HEIC: ".heic", JPEG: ".jpg", PNG: ".png", WebP: ".webp", "bogus": ""}
	for f, ext := range want {
		if got := f.Extension(); got != ext {
			t.Errorf("%q.Extension() = %q; want %q", f, got, ext)
		}
	}
}

func ftyp(major string, compatible ...string) []byte {
	b := []byte{0, 0, 0, 0}
	b = append(b, "ftyp"...)
	b = append(b, major...)
	b = append(b, 0, 0, 0, 0)
	for _, c := range compatible {
		b = append(b, c...)
	}
	binary.BigEndian.PutUint32(b[0:4], uint32(len(b)))
	return b
}

// ftypLarge builds an ftyp box that uses the 64-bit largesize field.
func ftypLarge(major string, compatible ...string) []byte {
	b := []byte{0, 0, 0, 1}
	b = append(b, "ftyp"...)
	b = append(b, 0, 0, 0, 0, 0, 0, 0, 0) // largesize, filled below
	b = append(b, major...)
	b = append(b, 0, 0, 0, 0)
	for _, c := range compatible {
		b = append(b, c...)
	}
	b[15] = byte(len(b))
	return b
}

// longBrandList returns n filler brands, pushing later brands past 256 bytes.
func longBrandList(n int) []string {
	brands := make([]string, n)
	for i := range brands {
		brands[i] = "zzzz"
	}
	return brands
}

func TestDetect(t *testing.T) {
	longFtyp := ftyp("mif1", append(longBrandList(100), "heic")...)
	// A box claiming to be larger than the bytes read must not panic.
	truncated := ftyp("mif1", "miaf", "heic")
	truncated[2] = 0x10

	tests := []struct {
		name   string
		header []byte
		want   Format
	}{
		{"heic largesize", ftypLarge("mif1", "heic"), HEIC},
		{"heic brand past 256 bytes", longFtyp, HEIC},
		{"box larger than header", truncated, HEIC},
		{"avif largesize rejected", ftypLarge("avif", "mif1"), ""},
		{"size too small", []byte{0, 0, 0, 8, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c', 0, 0, 0, 0}, ""},
		{"largesize too short", []byte{0, 0, 0, 1, 'f', 't', 'y', 'p', 0, 0}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Detect(tt.header)
			if tt.want == "" {
				if !errors.Is(err, ErrUnknown) {
					t.Fatalf("Detect = %q, %v; want ErrUnknown", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("Detect = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
	if len(longFtyp) <= 256 {
		t.Fatalf("long ftyp fixture is only %d bytes", len(longFtyp))
	}
}

func TestDetectBasic(t *testing.T) {
	tests := []struct {
		name   string
		header []byte
		want   Format
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, JPEG},
		{"png", []byte("\x89PNG\r\n\x1a\n...."), PNG},
		{"webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), WebP},
		{"heic major", ftyp("heic", "mif1"), HEIC},
		{"heic via compatible", ftyp("mif1", "miaf", "heic"), HEIC},
		{"avif rejected", ftyp("avif", "mif1", "miaf"), ""},
		{"generic mif1 rejected", ftyp("mif1", "miaf"), ""},
		{"mp4 rejected", ftyp("isom", "mp41"), ""},
		{"empty", nil, ""},
		{"text", []byte("hello world, not an image"), ""},
		{"riff wave", []byte("RIFF\x00\x00\x00\x00WAVEfmt "), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Detect(tt.header)
			if tt.want == "" {
				if !errors.Is(err, ErrUnknown) {
					t.Fatalf("Detect = %q, %v; want ErrUnknown", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("Detect = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestDetectFile(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "images")
	tests := map[string]Format{
		"heic/gradient.heic":   HEIC,
		"heic/display-p3.heic": HEIC,
		"jpeg/gradient.jpg":    JPEG,
		"png/alpha.png":        PNG,
		"webp/gradient.webp":   WebP,
		"invalid/empty.heic":   "",
		"invalid/text.jpg":     "",
	}
	for name, want := range tests {
		got, err := DetectFile(filepath.Join(dir, name))
		if want == "" {
			if !errors.Is(err, ErrUnknown) {
				t.Errorf("%s: got %q, %v; want ErrUnknown", name, got, err)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestDetectFileLongFtyp(t *testing.T) {
	header := ftyp("mif1", append(longBrandList(100), "heic")...)
	path := filepath.Join(t.TempDir(), "long.heic")
	// Pad with an mdat-like tail so the file is larger than the header.
	if err := os.WriteFile(path, append(header, make([]byte, 8192)...), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := DetectFile(path)
	if err != nil || got != HEIC {
		t.Fatalf("DetectFile = %q, %v; want HEIC", got, err)
	}
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file.heic")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDetectFileFtypBeyondInitialRead(t *testing.T) {
	// ~10 KB ftyp box whose only HEIC brand is past HeaderSize.
	brands := append(longBrandList(2500), "heic")
	tail := make([]byte, 1024) // following boxes

	t.Run("32-bit size", func(t *testing.T) {
		box := ftyp("mif1", brands...)
		if len(box) <= HeaderSize {
			t.Fatalf("box is only %d bytes", len(box))
		}
		got, err := DetectFile(writeTemp(t, append(box, tail...)))
		if err != nil || got != HEIC {
			t.Fatalf("DetectFile = %q, %v; want HEIC", got, err)
		}
	})

	t.Run("64-bit largesize", func(t *testing.T) {
		box := ftypLarge("mif1", brands...)
		binary.BigEndian.PutUint64(box[8:16], uint64(len(box)))
		got, err := DetectFile(writeTemp(t, append(box, tail...)))
		if err != nil || got != HEIC {
			t.Fatalf("DetectFile = %q, %v; want HEIC", got, err)
		}
	})

	t.Run("size 0 runs to end of file", func(t *testing.T) {
		box := ftyp("mif1", brands...)
		binary.BigEndian.PutUint32(box[0:4], 0)
		got, err := DetectFile(writeTemp(t, box))
		if err != nil || got != HEIC {
			t.Fatalf("DetectFile = %q, %v; want HEIC", got, err)
		}
	})

	t.Run("brand absent", func(t *testing.T) {
		box := ftyp("mif1", longBrandList(2500)...)
		if _, err := DetectFile(writeTemp(t, append(box, tail...))); !errors.Is(err, ErrUnknown) {
			t.Fatalf("err = %v; want ErrUnknown", err)
		}
	})
}

func TestDetectFileFtypTooLarge(t *testing.T) {
	box := ftyp("mif1", longBrandList(1100)...) // > HeaderSize so a re-read is needed
	binary.BigEndian.PutUint32(box[0:4], MaxFtypSize+4)
	_, err := DetectFile(writeTemp(t, box))
	if !errors.Is(err, ErrHeaderTooLarge) {
		t.Fatalf("err = %v; want ErrHeaderTooLarge", err)
	}

	huge := ftypLarge("mif1", longBrandList(1100)...)
	binary.BigEndian.PutUint64(huge[8:16], 1<<40)
	if _, err := DetectFile(writeTemp(t, huge)); !errors.Is(err, ErrHeaderTooLarge) {
		t.Fatalf("largesize err = %v; want ErrHeaderTooLarge", err)
	}
}

func FuzzDetect(f *testing.F) {
	f.Add([]byte{0xFF, 0xD8, 0xFF})
	f.Add(ftyp("mif1", "heic"))
	f.Add([]byte("RIFF\x00\x00\x00\x00WEBP"))
	f.Add([]byte{0, 0, 0, 0xFF, 'f', 't', 'y', 'p', 'm', 'i', 'f', '1', 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, b []byte) {
		got, err := Detect(b)
		if (err == nil) == (got == "") {
			t.Fatalf("Detect returned %q, %v", got, err)
		}
	})
}
