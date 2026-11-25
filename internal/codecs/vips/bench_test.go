package vips

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/format"
)

// Benchmark inputs are large and git-ignored; generate them with
// scripts/gen-bench-images.sh (run by `make bench`).
var benchDir = filepath.Join("..", "..", "..", "testdata", "bench")

const benchPixels = 4032 * 3024

func benchInput(b *testing.B, name string) string {
	b.Helper()
	path := filepath.Join(benchDir, name)
	if _, err := os.Stat(path); err != nil {
		if os.Getenv("FMTSHIFT_REQUIRE_BENCH_INPUTS") != "" {
			b.Fatalf("%s missing: %v", path, err)
		}
		b.Skipf("%s missing; run `make bench`", path)
	}
	return path
}

func benchCodec(b *testing.B) *Codec {
	b.Helper()
	c, err := New()
	if err != nil {
		b.Fatal(err)
	}
	return c
}

// reportThroughput adds megapixels per second; Go's -benchmem cannot see
// libvips' C allocations, so peak memory is measured separately.
func reportThroughput(b *testing.B, images int) {
	b.ReportMetric(float64(images)*benchPixels/1e6/b.Elapsed().Seconds(), "MP/s")
}

func BenchmarkConvert(b *testing.B) {
	c := benchCodec(b)
	cases := []struct {
		name  string
		input string
		dst   format.Format
		strip bool
	}{
		{"heic-12mp/jpeg", "12mp.heic", format.JPEG, false},
		{"heic-12mp/png", "12mp.heic", format.PNG, false},
		{"heic-12mp/webp", "12mp.heic", format.WebP, false},
		{"heic-12mp/jpeg-strip", "12mp.heic", format.JPEG, true},
		{"heic-12mp-10bit-sdr/jpeg", "12mp-10bit-sdr.heic", format.JPEG, false},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			src := benchInput(b, tc.input)
			dir := b.TempDir()
			opts := codecs.Options{Quality: 85, StripMetadata: tc.strip}
			n := 0
			for b.Loop() {
				dst := filepath.Join(dir, strconv.Itoa(n)+tc.dst.Extension())
				if err := convertPath(context.Background(), c, src, format.HEIC, dst, tc.dst, opts); err != nil {
					b.Fatal(err)
				}
				os.Remove(dst)
				n++
			}
			reportThroughput(b, n)
		})
	}
}

// BenchmarkConvertWorkers measures batch throughput at the default worker
// count (2) and at higher counts, to see how libvips threading and Go
// workers interact.
func BenchmarkConvertWorkers(b *testing.B) {
	c := benchCodec(b)
	src := benchInput(b, "12mp.heic")
	for _, workers := range []int{1, 2, 4} {
		b.Run("jpeg/workers="+strconv.Itoa(workers), func(b *testing.B) {
			dir := b.TempDir()
			var n atomic.Int64
			b.SetParallelism(workers)
			// SetParallelism multiplies by GOMAXPROCS; cap to the intended count.
			sem := make(chan struct{}, workers)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					sem <- struct{}{}
					i := n.Add(1)
					dst := filepath.Join(dir, strconv.FormatInt(i, 10)+".jpg")
					err := convertPath(context.Background(), c, src, format.HEIC, dst, format.JPEG, codecs.Options{Quality: 85})
					os.Remove(dst)
					<-sem
					if err != nil {
						b.Error(err)
						return
					}
				}
			})
			reportThroughput(b, int(n.Load()))
		})
	}
}
