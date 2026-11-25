package converter

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/filesystem"
)

// Destination returns the output path req would write, without converting
// or creating anything. ok is false when req would not produce an output
// because its source is missing, not a regular file (including a FIFO,
// which is rejected without blocking), unsupported or already in the target
// format; Convert reports the reason for those.
func (c *Converter) Destination(req Request) (path string, ok bool) {
	f, err := filesystem.DetectFormat(req.SourcePath)
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
//
// It reads each source's header, so for large batches it stops early when
// ctx is cancelled; the partial result is then irrelevant because no
// conversion will start.
func (c *Converter) DuplicateDestinations(ctx context.Context, reqs []Request) map[int]int {
	first := map[string]int{}
	dups := map[int]int{}
	for i, req := range reqs {
		if ctx.Err() != nil {
			break
		}
		dst, ok := c.Destination(req)
		if !ok {
			continue
		}
		key := PathKey(dst)
		if j, seen := first[key]; seen {
			dups[i] = j
			continue
		}
		first[key] = i
	}
	return dups
}

// PathKey normalises a file path for "is this the same directory entry"
// comparisons between sources and destinations: absolute, with symlinks in
// its directory resolved where the directory exists, and case-folded to
// match case-insensitive filesystems (conservative on case-sensitive ones).
// The final component is not resolved, because publishing replaces the
// directory entry itself, never the file a symlink points to.
func PathKey(path string) string {
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
