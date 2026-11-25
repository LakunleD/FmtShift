//go:build !unix

package converter

import "os"

func openSource(path string) (*os.File, error) {
	return os.Open(path)
}
