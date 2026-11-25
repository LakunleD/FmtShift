// Package filesystem provides safe output publication.
package filesystem

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrExists is returned when the destination already exists.
var ErrExists = errors.New("destination already exists")

// outputMode is applied to published files; temporary files are created 0600.
const outputMode fs.FileMode = 0o644

// WriteNew creates dst without ever replacing an existing file.
//
// write is called with the path of a temporary file in dst's directory, so
// the result lands on the same filesystem. On success the temporary file is
// hard-linked to dst, which fails atomically if dst appeared in the meantime.
// The temporary file is always removed; dst is never deleted or modified if
// it already exists.
//
// Filesystems without hard-link support (some FAT, exFAT and network
// volumes) return an error; a fallback is future work.
func WriteNew(dst string, write func(tmp string) error) error {
	f, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*.fmtshift-tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Close(); err != nil {
		return err
	}

	if err := write(tmp); err != nil {
		return err
	}
	if err := os.Chmod(tmp, outputMode); err != nil {
		return err
	}
	if err := os.Link(tmp, dst); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: %s", ErrExists, dst)
		}
		return err
	}
	return nil
}
