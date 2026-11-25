package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func writeString(s string) func(string) error {
	return func(tmp string) error { return os.WriteFile(tmp, []byte(s), 0o600) }
}

// entries returns the names in dir, to catch leftover temporary files.
func entries(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names
}

func TestWriteNew(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.jpg")
	if err := WriteNew(dst, writeString("new")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "new" {
		t.Fatalf("content = %q", b)
	}
	if got := entries(t, dir); len(got) != 1 {
		t.Errorf("directory contains %v; want only out.jpg", got)
	}
}

func TestWriteNewKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.jpg")
	if err := os.WriteFile(dst, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := WriteNew(dst, writeString("new"))
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v; want ErrExists", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "original" {
		t.Fatalf("existing file changed to %q", b)
	}
	if got := entries(t, dir); len(got) != 1 {
		t.Errorf("directory contains %v; temp file left behind", got)
	}
}

func TestWriteNewDestinationAppearsDuringWrite(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.jpg")
	err := WriteNew(dst, func(tmp string) error {
		// Another process creates dst while we are converting.
		if err := os.WriteFile(dst, []byte("racer"), 0o644); err != nil {
			return err
		}
		return os.WriteFile(tmp, []byte("new"), 0o600)
	})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v; want ErrExists", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "racer" {
		t.Fatalf("racing file changed to %q", b)
	}
}

func TestWriteNewFailedWrite(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.jpg")
	boom := errors.New("boom")
	err := WriteNew(dst, func(tmp string) error {
		os.WriteFile(tmp, []byte("partial"), 0o600)
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; want boom", err)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Errorf("directory contains %v; want nothing", got)
	}
}

func TestWriteNewConcurrent(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.jpg")
	const n = 20
	var wg sync.WaitGroup
	results := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- WriteNew(dst, writeString("x"))
		}()
	}
	wg.Wait()
	close(results)
	ok := 0
	for err := range results {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, ErrExists):
			t.Error(err)
		}
	}
	if ok != 1 {
		t.Errorf("%d writers succeeded; want exactly 1", ok)
	}
	if got := entries(t, dir); len(got) != 1 {
		t.Errorf("directory contains %v", got)
	}
}

func TestWriteNewMissingDirectory(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "missing", "out.jpg")
	if err := WriteNew(dst, writeString("x")); err == nil {
		t.Fatal("expected error for missing directory")
	}
}
