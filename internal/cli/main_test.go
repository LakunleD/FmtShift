package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/lakunled/fmtshift/internal/config"
)

// TestMain points the configuration file at a temporary location, so tests
// never read or modify the developer's real settings. Tests that exercise
// configuration use their own path via t.Setenv.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fmtshift-cli-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv(config.EnvPath, filepath.Join(dir, "config.json"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
