package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/lakunled/fmtshift/internal/format"
)

func tempPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "fmtshift", "config.json")
}

func write(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o700)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPath(t *testing.T) {
	t.Setenv(EnvPath, "")
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p, filepath.Join("fmtshift", "config.json")) {
		t.Errorf("default path %q", p)
	}
	if runtime.GOOS == "darwin" && !strings.Contains(p, filepath.Join("Library", "Application Support")) {
		t.Errorf("macOS path %q is not under Application Support", p)
	}

	t.Setenv(EnvPath, "/tmp/custom.json")
	if p, _ := Path(); p != "/tmp/custom.json" {
		t.Errorf("override ignored: %q", p)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	c, err := Load(tempPath(t))
	if err != nil {
		t.Fatal(err)
	}
	e := c.Effective()
	if e.Format != format.JPEG || e.Quality != 85 || e.Workers != 2 || e.Output != "" || e.StripMetadata {
		t.Errorf("defaults = %+v", e)
	}
}

func TestLoadValid(t *testing.T) {
	p := tempPath(t)
	write(t, p, `{"version":1,"format":"png","quality":70,"workers":4,"output":"/srv/out","stripMetadata":true}`)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	want := Effective{Format: format.PNG, Quality: 70, Workers: 4, Output: "/srv/out", StripMetadata: true}
	if e := c.Effective(); e != want {
		t.Errorf("effective = %+v; want %+v", e, want)
	}
}

func TestLoadInvalid(t *testing.T) {
	for name, content := range map[string]string{
		"not json":         `format = jpeg`,
		"unknown key":      `{"version":1,"overwrite":true}`,
		"no version":       `{"format":"png"}`,
		"future version":   `{"version":2}`,
		"heic output":      `{"version":1,"format":"heic"}`,
		"bad quality":      `{"version":1,"quality":500}`,
		"bad workers":      `{"version":1,"workers":-1}`,
		"relative output":  `{"version":1,"output":"out"}`,
		"trailing garbage": `{"version":1} {"version":1}`,
		"stray ]":          `{"version":1}]`,
		"stray }":          `{"version":1}}`,
		"trailing word":    `{"version":1} x`,
		"wrong type":       `{"version":1,"quality":"high"}`,
	} {
		t.Run(name, func(t *testing.T) {
			p := tempPath(t)
			write(t, p, content)
			if _, err := Load(p); !errors.Is(err, ErrInvalid) {
				t.Errorf("err = %v; want ErrInvalid", err)
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	p := tempPath(t)
	for _, kv := range [][2]string{{"format", "webp"}, {"quality", "92"}, {"workers", "3"}, {"strip-metadata", "true"}} {
		k, err := LookupKey(kv[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := Update(p, func(c *Config) error { return k.Set(c, kv[1]) }); err != nil {
			t.Fatalf("set %s: %v", kv[0], err)
		}
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	want := Effective{Format: format.WebP, Quality: 92, Workers: 3, StripMetadata: true}
	if e := c.Effective(); e != want {
		t.Errorf("effective = %+v; want %+v", e, want)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".config-") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

// An invalid file is reported, never replaced by a fresh one.
func TestUpdateKeepsInvalidFile(t *testing.T) {
	p := tempPath(t)
	broken := `{"version":1, "quality": 85,`
	write(t, p, broken)
	k, _ := LookupKey("quality")
	err := Update(p, func(c *Config) error { return k.Set(c, "90") })
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v; want ErrInvalid", err)
	}
	if b, _ := os.ReadFile(p); string(b) != broken {
		t.Errorf("invalid file was overwritten with %q", b)
	}
}

// Concurrent read-modify-write updates must not lose changes.
func TestConcurrentUpdates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no file locking on Windows yet")
	}
	p := tempPath(t)
	const n = 50
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := Update(p, func(c *Config) error {
				c.Workers++ // 0 (unset) -> 1 -> ... -> n
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Workers != n {
		t.Errorf("workers = %d after %d concurrent increments; updates were lost", c.Workers, n)
	}
}

func TestKeys(t *testing.T) {
	var c Config
	set := func(key, value string) error {
		k, err := LookupKey(key)
		if err != nil {
			return err
		}
		return k.Set(&c, value)
	}

	for _, bad := range [][2]string{
		{"format", "heic"}, {"format", "gif"}, {"quality", "0"}, {"quality", "abc"},
		{"workers", "65"}, {"strip-metadata", "maybe"}, {"colour", "red"},
	} {
		if err := set(bad[0], bad[1]); !errors.Is(err, ErrInvalid) {
			t.Errorf("set %s=%s: err = %v; want ErrInvalid", bad[0], bad[1], err)
		}
	}

	_, err := LookupKey("overwrite")
	if err == nil || !strings.Contains(err.Error(), "requires --overwrite on the command") {
		t.Errorf("overwrite: err = %v", err)
	}

	if err := set("format", "JPG"); err != nil || c.Format != "jpeg" {
		t.Errorf("format JPG: %v, stored %q", err, c.Format)
	}

	wd, _ := os.Getwd()
	if err := set("output", "converted"); err != nil || c.Output != filepath.Join(wd, "converted") {
		t.Errorf("relative output stored as %q (%v); want absolute", c.Output, err)
	}
	home, _ := os.UserHomeDir()
	if err := set("output", "~/Pictures/out"); err != nil || c.Output != filepath.Join(home, "Pictures", "out") {
		t.Errorf("~ output stored as %q (%v)", c.Output, err)
	}
	if err := set("output", ""); err != nil || c.Output != "" {
		t.Errorf("clearing output: %q, %v", c.Output, err)
	}
}

// Trailing whitespace (including a final newline) is fine.
func TestLoadAllowsTrailingWhitespace(t *testing.T) {
	p := tempPath(t)
	write(t, p, "{\"version\":1,\"quality\":90}\n\n  \t\n")
	if c, err := Load(p); err != nil || c.Quality != 90 {
		t.Fatalf("Load = %+v, %v", c, err)
	}
}

// The reviewer's case end to end: a file with a stray closing bracket must
// not be replaced by an update.
func TestUpdateKeepsFileWithStrayClosingToken(t *testing.T) {
	p := tempPath(t)
	content := `{"version":1}]`
	write(t, p, content)
	k, _ := LookupKey("quality")
	if err := Update(p, func(c *Config) error { return k.Set(c, "90") }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v; want ErrInvalid", err)
	}
	if b, _ := os.ReadFile(p); string(b) != content {
		t.Errorf("file was rewritten to %q", b)
	}
}
