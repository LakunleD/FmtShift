package vips

import (
	"errors"
	"testing"

	"github.com/lakunled/fmtshift/internal/codecs"
)

func TestHEIFColourPolicy(t *testing.T) {
	tests := []struct {
		name    string
		in      heifColour
		profile string
		err     error
	}{
		{"no colour boxes", heifColour{false, noNCLX, noNCLX}, "", nil},
		{"icc only", heifColour{true, noNCLX, noNCLX}, "", nil},
		{"nclx srgb", heifColour{false, primariesBT709, transferSRGB}, "", nil},
		{"nclx bt709 unspecified transfer", heifColour{false, primariesBT709, transferUnspecified}, "", nil},
		{"nclx bt601", heifColour{false, primariesBT601_525, transferBT601}, "", nil},
		{"nclx p3", heifColour{false, primariesDisplayP3, transferSRGB}, "p3", nil},
		{"icc wins over nclx p3", heifColour{true, primariesDisplayP3, transferSRGB}, "", nil},
		{"pq", heifColour{false, 9, transferPQ}, "", codecs.ErrHDR},
		{"hlg", heifColour{false, 9, transferHLG}, "", codecs.ErrHDR},
		{"pq rejected despite icc", heifColour{true, 9, transferPQ}, "", codecs.ErrHDR},
		{"bt2020 sdr", heifColour{false, 9, transferBT2020_10}, "", codecs.ErrColourSpace},
		{"bt2020 sdr with icc", heifColour{true, 9, transferBT2020_10}, "", nil},
		{"dci-p3 primaries", heifColour{false, 11, transferSRGB}, "", codecs.ErrColourSpace},
		{"linear transfer", heifColour{false, primariesBT709, 8}, "", codecs.ErrColourSpace},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile, err := heifColourPolicy(tt.in)
			if !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("err = %v; want %v", err, tt.err)
			}
			if profile != tt.profile {
				t.Errorf("profile = %q; want %q", profile, tt.profile)
			}
		})
	}
}
