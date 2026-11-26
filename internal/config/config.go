// Package config stores the user's default conversion settings as JSON in
// the platform's per-user configuration directory.
//
// Precedence is decided by callers: explicit command-line flags, then saved
// configuration, then built-in defaults. Overwriting existing files is
// deliberately not configurable, so replacing files always requires an
// explicit --overwrite on the command.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/lakunled/fmtshift/internal/format"
)

// EnvPath overrides the configuration file location when set.
const EnvPath = "FMTSHIFT_CONFIG"

// Version is the current file format version.
const Version = 1

// Built-in defaults, used when neither a flag nor the file sets a value.
const (
	DefaultFormat  = format.JPEG
	DefaultQuality = 85
	DefaultWorkers = 2
	MaxWorkers     = 64
)

// ErrInvalid means the configuration file or a value is invalid. A file
// that fails to load is never overwritten automatically.
var ErrInvalid = errors.New("invalid configuration")

// writable are the formats a saved default may name.
var writable = []format.Format{format.JPEG, format.PNG, format.WebP}

// Config holds saved settings. Zero values mean "not set".
type Config struct {
	Version       int    `json:"version"`
	Format        string `json:"format,omitempty"`
	Quality       int    `json:"quality,omitempty"`
	Workers       int    `json:"workers,omitempty"`
	Output        string `json:"output,omitempty"`
	StripMetadata *bool  `json:"stripMetadata,omitempty"`
}

// Effective is the settings in force after applying defaults.
type Effective struct {
	Format        format.Format `json:"format"`
	Quality       int           `json:"quality"`
	Workers       int           `json:"workers"`
	Output        string        `json:"output"`
	StripMetadata bool          `json:"stripMetadata"`
}

// Effective fills unset values with built-in defaults.
func (c Config) Effective() Effective {
	e := Effective{Format: DefaultFormat, Quality: DefaultQuality, Workers: DefaultWorkers}
	if c.Format != "" {
		if f, err := format.Parse(c.Format); err == nil {
			e.Format = f
		}
	}
	if c.Quality != 0 {
		e.Quality = c.Quality
	}
	if c.Workers != 0 {
		e.Workers = c.Workers
	}
	e.Output = c.Output
	if c.StripMetadata != nil {
		e.StripMetadata = *c.StripMetadata
	}
	return e
}

// Path returns the configuration file path: $FMTSHIFT_CONFIG if set,
// otherwise fmtshift/config.json in the user configuration directory
// (macOS ~/Library/Application Support, Linux $XDG_CONFIG_HOME or
// ~/.config, Windows %AppData%).
func Path() (string, error) {
	if p := os.Getenv(EnvPath); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot find the user configuration directory: %w", err)
	}
	return filepath.Join(dir, "fmtshift", "config.json"), nil
}

// Load reads and validates the file at path. A missing file is not an
// error and yields an empty Config.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{Version: Version}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("%w: %s: %v", ErrInvalid, path, err)
	}
	// Anything but whitespace after the object makes the file invalid.
	// (Decoder.More is for iterating arrays and objects, and reports false
	// for a stray closing "]" or "}".)
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("%w: %s: unexpected data after the JSON object", ErrInvalid, path)
	}
	if c.Version != Version {
		return Config{}, fmt.Errorf("%w: %s: unsupported version %d (want %d)", ErrInvalid, path, c.Version, Version)
	}
	if err := c.validate(); err != nil {
		return Config{}, fmt.Errorf("%w: %s: %v", ErrInvalid, path, err)
	}
	return c, nil
}

func (c Config) validate() error {
	if c.Format != "" {
		if err := checkFormat(c.Format); err != nil {
			return err
		}
	}
	if c.Quality != 0 {
		if err := checkQuality(c.Quality); err != nil {
			return err
		}
	}
	if c.Workers != 0 {
		if err := checkWorkers(c.Workers); err != nil {
			return err
		}
	}
	if c.Output != "" && !filepath.IsAbs(c.Output) {
		return fmt.Errorf("output %q must be an absolute path", c.Output)
	}
	return nil
}

// Update applies fn to the saved configuration and saves the result. It
// holds an exclusive lock for the whole read-modify-write, so concurrent
// updates are not lost, and saves atomically, so the file is never left
// half-written. If the existing file is invalid, it is left untouched and
// the load error is returned.
func Update(path string, fn func(*Config) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	unlock, err := lock(path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()

	c, err := Load(path)
	if err != nil {
		return err
	}
	if err := fn(&c); err != nil {
		return err
	}
	c.Version = Version
	if err := c.validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return save(path, c)
}

// save writes c to a temporary file in the same directory, syncs it and
// renames it over path.
func save(path string, c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Key describes one settable configuration key.
type Key struct {
	Name        string
	Description string
	set         func(c *Config, value string) error
	get         func(e Effective) string
}

// Keys lists the settable keys in display order.
var Keys = []Key{
	{
		Name:        "format",
		Description: "default output format: jpeg, png or webp",
		set: func(c *Config, v string) error {
			if err := checkFormat(v); err != nil {
				return err
			}
			f, _ := format.Parse(v)
			c.Format = string(f)
			return nil
		},
		get: func(e Effective) string { return string(e.Format) },
	},
	{
		Name:        "quality",
		Description: "default JPEG/WebP quality, 1-100",
		set: func(c *Config, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("quality must be a whole number, got %q", v)
			}
			if err := checkQuality(n); err != nil {
				return err
			}
			c.Quality = n
			return nil
		},
		get: func(e Effective) string { return strconv.Itoa(e.Quality) },
	},
	{
		Name:        "workers",
		Description: fmt.Sprintf("default number of concurrent conversions, 1-%d", MaxWorkers),
		set: func(c *Config, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("workers must be a whole number, got %q", v)
			}
			if err := checkWorkers(n); err != nil {
				return err
			}
			c.Workers = n
			return nil
		},
		get: func(e Effective) string { return strconv.Itoa(e.Workers) },
	},
	{
		Name:        "output",
		Description: "default output directory (stored as an absolute path; empty = next to sources)",
		set: func(c *Config, v string) error {
			if v == "" {
				c.Output = ""
				return nil
			}
			abs, err := filepath.Abs(expandHome(v))
			if err != nil {
				return err
			}
			c.Output = abs
			return nil
		},
		get: func(e Effective) string { return e.Output },
	},
	{
		Name:        "strip-metadata",
		Description: "remove EXIF, location and other metadata by default: true or false",
		set: func(c *Config, v string) error {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("strip-metadata must be true or false, got %q", v)
			}
			c.StripMetadata = &b
			return nil
		},
		get: func(e Effective) string { return strconv.FormatBool(e.StripMetadata) },
	},
}

// LookupKey finds a key by name.
func LookupKey(name string) (Key, error) {
	for _, k := range Keys {
		if k.Name == name {
			return k, nil
		}
	}
	names := make([]string, len(Keys))
	for i, k := range Keys {
		names[i] = k.Name
	}
	msg := fmt.Sprintf("unknown configuration key %q (valid keys: %s)", name, strings.Join(names, ", "))
	if name == "overwrite" {
		msg = `"overwrite" cannot be saved: replacing files always requires --overwrite on the command`
	}
	return Key{}, fmt.Errorf("%w: %s", ErrInvalid, msg)
}

// Set validates value and stores it in c.
func (k Key) Set(c *Config, value string) error {
	if err := k.set(c, value); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return nil
}

// Get returns the key's value from effective settings.
func (k Key) Get(e Effective) string { return k.get(e) }

func checkFormat(v string) error {
	f, err := format.Parse(v)
	if err != nil || !slices.Contains(writable, f) {
		return fmt.Errorf("format must be jpeg, png or webp, got %q", v)
	}
	return nil
}

func checkQuality(n int) error {
	if n < 1 || n > 100 {
		return fmt.Errorf("quality must be between 1 and 100, got %d", n)
	}
	return nil
}

func checkWorkers(n int) error {
	if n < 1 || n > MaxWorkers {
		return fmt.Errorf("workers must be between 1 and %d, got %d", MaxWorkers, n)
	}
	return nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
