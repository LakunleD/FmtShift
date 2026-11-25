package format

import (
	"errors"
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
	b[3] = byte(len(b))
	return b
}

func TestDetect(t *testing.T) {
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
