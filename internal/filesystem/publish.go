// Package filesystem provides safe output publication.
package filesystem

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
)

// ErrExists is returned when the destination already exists.
var ErrExists = errors.New("destination already exists")

// createMode is the mode requested for new files. The operating system
// applies the user's umask, exactly as for any other newly created file, so
// a umask of 077 keeps converted photos private.
const createMode fs.FileMode = 0o666

// WriteNew creates dst without ever replacing an existing file.
//
// write is called with the path of an empty temporary file in dst's
// directory, so the result lands on the same filesystem. write must replace
// the file's contents in place (truncate and write) rather than delete and
// recreate it, so the umask-derived permissions are kept. On success the
// temporary file is hard-linked to dst, which fails atomically if dst
// appeared in the meantime. The temporary file is always removed; dst is
// never deleted or modified if it already exists.
//
// Filesystems without hard-link support (some FAT, exFAT and network
// volumes) return an error; a fallback is future work.
func WriteNew(dst string, write func(tmp string) error) error {
	tmp, err := createTemp(dst)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)

	if err := write(tmp); err != nil {
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

// createTemp creates a new, empty file next to dst with createMode (subject
// to umask). os.CreateTemp is not used because it always creates files 0600.
func createTemp(dst string) (string, error) {
	dir, base := filepath.Split(dst)
	for range 100 {
		name := filepath.Join(dir, "."+base+"."+strconv.FormatUint(rand.Uint64(), 36)+".fmtshift-tmp")
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, createMode)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if err := f.Close(); err != nil {
			os.Remove(name)
			return "", err
		}
		return name, nil
	}
	return "", fmt.Errorf("cannot create temporary file for %s", dst)
}
