package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lakunled/fmtshift/internal/config"
	"github.com/lakunled/fmtshift/internal/format"
)

// isolatedConfig gives the test its own configuration file path.
func isolatedConfig(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cfg", "config.json")
	t.Setenv(config.EnvPath, p)
	return p
}

func TestConfigPathAndDefaults(t *testing.T) {
	p := isolatedConfig(t)
	r := fmtshift(t, "config", "path")
	r.expect(t, ExitOK)
	if strings.TrimSpace(r.stdout) != p {
		t.Errorf("path = %q; want %q", r.stdout, p)
	}

	r = fmtshift(t, "config", "show")
	r.expect(t, ExitOK)
	var got config.Effective
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
		t.Fatalf("show is not JSON: %v\n%s", err, r.stdout)
	}
	if got != (config.Effective{Format: format.JPEG, Quality: 85, Workers: 2}) {
		t.Errorf("defaults = %+v", got)
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("reading settings created the file")
	}
}

func TestConfigSetGet(t *testing.T) {
	isolatedConfig(t)
	for _, kv := range [][2]string{{"format", "png"}, {"quality", "92"}, {"workers", "3"}, {"strip-metadata", "true"}} {
		fmtshift(t, "config", "set", kv[0], kv[1]).expect(t, ExitOK)
		r := fmtshift(t, "config", "get", kv[0])
		r.expect(t, ExitOK)
		if strings.TrimSpace(r.stdout) != kv[1] {
			t.Errorf("get %s = %q; want %q", kv[0], r.stdout, kv[1])
		}
	}
}

func TestConfigSetRejects(t *testing.T) {
	p := isolatedConfig(t)
	for name, tc := range map[string]struct {
		args []string
		msg  string
	}{
		"bad quality": {[]string{"config", "set", "quality", "500"}, "quality must be between 1 and 100"},
		"heic":        {[]string{"config", "set", "format", "heic"}, "format must be jpeg, png or webp"},
		"unknown key": {[]string{"config", "set", "colour", "red"}, `unknown configuration key "colour"`},
		"overwrite":   {[]string{"config", "set", "overwrite", "true"}, "requires --overwrite on the command"},
		"get unknown": {[]string{"config", "get", "nope"}, "unknown configuration key"},
	} {
		t.Run(name, func(t *testing.T) {
			r := fmtshift(t, tc.args...)
			r.expect(t, ExitUsage)
			if !strings.Contains(r.stderr, tc.msg) {
				t.Errorf("stderr=%q; want %q", r.stderr, tc.msg)
			}
		})
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("a rejected value created the configuration file")
	}
}

// Flags typed on the command line beat saved settings, which beat defaults.
func TestConvertUsesSavedSettings(t *testing.T) {
	isolatedConfig(t)
	dir := workdir(t, map[string]string{"a.heic": "heic/display-p3.heic", "b.heic": "heic/display-p3.heic"})
	out := filepath.Join(dir, "saved-out")
	fmtshift(t, "config", "set", "format", "png").expect(t, ExitOK)
	fmtshift(t, "config", "set", "output", out).expect(t, ExitOK)
	fmtshift(t, "config", "set", "strip-metadata", "true").expect(t, ExitOK)

	// Saved format and output apply.
	fmtshift(t, "convert", "a.heic").expect(t, ExitOK)
	if f, err := format.DetectFile(filepath.Join(out, "a.png")); err != nil || f != format.PNG {
		t.Fatalf("saved settings not applied: %q, %v", f, err)
	}

	// Typed flags win, including turning a saved true off.
	fmtshift(t, "convert", "b.heic", "--to", "jpeg", "-o", "flag-out", "--strip-metadata=false").expect(t, ExitOK)
	if f, err := format.DetectFile(filepath.Join(dir, "flag-out", "b.jpg")); err != nil || f != format.JPEG {
		t.Fatalf("flags did not override saved settings: %q, %v", f, err)
	}
}

// A broken file is reported with exit 3 and never replaced.
func TestBrokenConfigFile(t *testing.T) {
	p := isolatedConfig(t)
	os.MkdirAll(filepath.Dir(p), 0o700)
	broken := `{"version":1,"quality":`
	os.WriteFile(p, []byte(broken), 0o600)
	workdir(t, map[string]string{"a.heic": "heic/gradient.heic"})

	for _, args := range [][]string{
		{"convert", "a.heic"},
		{"config", "set", "quality", "90"},
		{"config", "show"},
		{"config", "get", "quality"},
	} {
		r := fmtshift(t, args...)
		r.expect(t, ExitConfig)
		if !strings.Contains(r.stderr, "invalid configuration: "+p) || !strings.Contains(r.stderr, "will not be changed automatically") {
			t.Errorf("%v: stderr=%q", args, r.stderr)
		}
	}
	if b, _ := os.ReadFile(p); string(b) != broken {
		t.Errorf("broken file was changed to %q", b)
	}
	if _, err := os.Stat("a.jpg"); err == nil {
		t.Error("convert ran with a broken configuration")
	}
	// Commands that do not use settings still work.
	fmtshift(t, "formats").expect(t, ExitOK)
	fmtshift(t, "version").expect(t, ExitOK)
}

// An invalid value typed on the command line is a usage error (2) even when
// the configuration file is broken; valid flags then hit the config error (3).
func TestTypedFlagErrorsBeatBrokenConfig(t *testing.T) {
	p := isolatedConfig(t)
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, []byte(`{"version":1,"quality":`), 0o600)
	workdir(t, map[string]string{"a.heic": "heic/gradient.heic"})

	for name, args := range map[string][]string{
		"quality": {"convert", "a.heic", "--quality", "500"},
		"workers": {"convert", "a.heic", "-w", "0"},
		"to":      {"convert", "a.heic", "--to", "gif"},
		"to heic": {"convert", "a.heic", "--to", "heic"},
		"from":    {"convert", "a.heic", "--from", "bmp"},
	} {
		t.Run(name, func(t *testing.T) {
			r := fmtshift(t, args...)
			r.expect(t, ExitUsage)
			if strings.Contains(r.stderr, "invalid configuration") {
				t.Errorf("reported the config file instead of the flag: %q", r.stderr)
			}
		})
	}
	fmtshift(t, "convert", "a.heic", "--quality", "90").expect(t, ExitConfig)
}
