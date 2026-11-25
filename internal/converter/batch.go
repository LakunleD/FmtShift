package converter

import (
	"path/filepath"
	"strings"

	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/format"
)

// Destination returns the output path req would write, without converting
// or creating anything. ok is false when req would not produce an output
// because its source is missing, unreadable, unsupported or already in the
// target format; Convert reports the reason for those.
func (c *Converter) Destination(req Request) (path string, ok bool) {
	f, err := format.DetectFile(req.SourcePath)
	if err != nil || f == req.Format || !codecs.CanRead(c.caps, f) {
		return "", false
	}
	return filepath.Join(outputDir(req), outputName(req.SourcePath, req.Format)), true
}

// DuplicateDestinations finds requests in a batch that would write the same
// output as an earlier request, such as IMG_1.HEIC and IMG_1.PNG converted to
// JPEG in one folder, or a/photo.heic and b/photo.heic into one --output
// directory. It maps the index of each duplicate to the index of the first
// request with that destination.
//
// Callers should fail the duplicates before converting anything, whatever
// the Overwrite setting: with Overwrite, later conversions would silently
// replace earlier results; without it, they would be misreported as
// "output already exists".
//
// Destinations are compared after resolving symlinks in the output directory
// and ignoring case, matching the default macOS and Windows filesystems. On
// a case-sensitive filesystem this is conservative: names differing only in
// case are reported as duplicates although they could coexist.
func (c *Converter) DuplicateDestinations(reqs []Request) map[int]int {
	first := map[string]int{}
	dups := map[int]int{}
	for i, req := range reqs {
		dst, ok := c.Destination(req)
		if !ok {
			continue
		}
		key := destinationKey(dst)
		if j, seen := first[key]; seen {
			dups[i] = j
			continue
		}
		first[key] = i
	}
	return dups
}

// destinationKey normalises an output path for comparison: absolute, with
// symlinks in its directory resolved where the directory exists, and
// case-folded.
func destinationKey(path string) string {
	dir, name := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return strings.ToLower(filepath.Join(dir, name))
}
