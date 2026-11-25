//go:build !unix

package filesystem

import "os"

func openNonBlocking(path string) (*os.File, error) {
	return os.Open(path)
}
