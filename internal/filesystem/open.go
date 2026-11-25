package filesystem

import (
	"errors"
	"fmt"
	"os"

	"github.com/lakunled/fmtshift/internal/format"
)

// DetectFormat identifies the content format of the regular file at path,
// opening it with OpenRegular so it never blocks on a FIFO. Use it instead
// of format.DetectFile for any user-supplied input.
func DetectFormat(path string) (format.Format, error) {
	f, err := OpenRegular(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return format.DetectReaderAt(f)
}

// ErrNotRegular is returned by OpenRegular for directories, pipes, devices
// and other non-regular files.
var ErrNotRegular = errors.New("not a regular file")

// OpenRegular opens path for reading and verifies, on the open handle, that
// it is a regular file. Every input FmtShift reads goes through it.
//
// On Unix the open is non-blocking, so a FIFO placed at path is rejected
// immediately instead of blocking until a writer appears (which would hang
// a conversion, a batch preflight or a directory scan). O_NONBLOCK has no
// effect on reading regular files.
func OpenRegular(path string) (*os.File, error) {
	f, err := openNonBlocking(path)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%w: %s", ErrNotRegular, path)
	}
	return f, nil
}
