// Command fmtshift-poc is the M0 codec proof of concept. It converts a single
// image and will be replaced by the real CLI in M2.
//
//	fmtshift-poc [-q quality] [-strip] <input> <output>
//
// The output format is taken from the output file extension.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/codecs/vips"
	"github.com/lakunled/fmtshift/internal/format"
)

func main() {
	quality := flag.Int("q", 85, "lossy quality (1-100)")
	strip := flag.Bool("strip", false, "remove metadata")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: fmtshift-poc [-q quality] [-strip] <input> <output>")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(flag.Arg(0), flag.Arg(1), *quality, *strip); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(src, dst string, quality int, strip bool) error {
	srcFmt, err := format.DetectFile(src)
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	dstFmt, err := format.Parse(filepath.Ext(dst))
	if err != nil {
		return fmt.Errorf("output %s: %w", dst, err)
	}
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s already exists", dst)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	codec, err := vips.New()
	if err != nil {
		return err
	}

	start := time.Now()
	err = codec.Convert(context.Background(), src, srcFmt, dst, dstFmt, codecs.Options{
		Quality:       quality,
		StripMetadata: strip,
	})
	if err != nil {
		os.Remove(dst) // libvips may leave a partial file; we created it.
		return err
	}
	fmt.Printf("Converted %s -> %s (%s, %s)\n", src, dst, codec.Name(), time.Since(start).Round(time.Millisecond))
	return nil
}
