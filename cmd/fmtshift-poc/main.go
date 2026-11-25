// Command fmtshift-poc is the M0 codec proof of concept. It converts a single
// image and will be replaced by the real CLI in M2.
//
//	fmtshift-poc [-q quality] [-strip] <input> <output>
//
// The output format is taken from the output file extension.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/codecs/vips"
	"github.com/lakunled/fmtshift/internal/filesystem"
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
	codec, err := vips.New()
	if err != nil {
		return err
	}

	start := time.Now()
	err = filesystem.WriteNew(dst, func(tmp string) error {
		return codec.Convert(context.Background(), src, srcFmt, tmp, dstFmt, codecs.Options{
			Quality:       quality,
			StripMetadata: strip,
		})
	})
	if err != nil {
		return err
	}
	fmt.Printf("Converted %s -> %s (%s, %s)\n", src, dst, codec.Name(), time.Since(start).Round(time.Millisecond))
	return nil
}
