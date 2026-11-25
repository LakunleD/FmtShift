//go:build unix

package converter

import (
	"os"
	"syscall"
)

// openSource opens path for reading without blocking: opening a FIFO for
// reading would otherwise wait for a writer, hanging the conversion if a
// pipe is placed at the source path. O_NONBLOCK has no effect on regular
// files, and non-regular files are rejected after opening.
func openSource(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
