// Package e2e builds the fmtshift binary and runs it as a user would.
package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

var (
	binary   string
	fixtures string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fmtshift-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "fmtshift")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "github.com/lakunled/fmtshift/cmd/fmtshift")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "building fmtshift:", err)
		os.Exit(1)
	}
	fixtures, _ = filepath.Abs(filepath.Join("..", "..", "testdata"))
	// Never let the binary under test read or write the real user settings.
	os.Setenv("FMTSHIFT_CONFIG", filepath.Join(dir, "config.json"))

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	code           int
	stdout, stderr string
}

func fmtshift(t *testing.T, dir string, args ...string) result {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result{code, out.String(), errOut.String()}
}

func copyInto(t *testing.T, dir, fixture, name string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtures, fixture))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVersionAndFormats(t *testing.T) {
	r := fmtshift(t, t.TempDir(), "version")
	if r.code != 0 || !strings.HasPrefix(r.stdout, "FmtShift ") {
		t.Fatalf("version: %+v", r)
	}
	r = fmtshift(t, t.TempDir(), "formats")
	if r.code != 0 || !strings.Contains(r.stdout, "HEIC      Yes     No") {
		t.Fatalf("formats: %+v", r)
	}
}

func TestPlanAcceptance(t *testing.T) {
	// The M2 acceptance commands from docs/IMPLEMENTATION_PLAN.md.
	dir := t.TempDir()
	copyInto(t, dir, "images/heic/gradient.heic", "photo.heic")
	src, _ := os.ReadFile(filepath.Join(dir, "photo.heic"))

	if r := fmtshift(t, dir, "convert", "photo.heic", "--to", "jpeg"); r.code != 0 || r.stdout != "Converted photo.heic -> photo.jpg\n" {
		t.Fatalf("%+v", r)
	}
	if r := fmtshift(t, dir, "convert", "photo.heic", "--to", "png", "--output", "./converted"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	for _, out := range []string{"photo.jpg", "converted/photo.png"} {
		if fi, err := os.Stat(filepath.Join(dir, out)); err != nil || fi.Size() == 0 {
			t.Errorf("%s: %v", out, err)
		}
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "photo.heic")); !bytes.Equal(src, after) {
		t.Fatal("source modified")
	}
}

func TestExitCodes(t *testing.T) {
	dir := t.TempDir()
	copyInto(t, dir, "images/heic/gradient.heic", "ok.heic")
	copyInto(t, dir, "images/invalid/truncated.heic", "bad.heic")
	copyInto(t, dir, "images/jpeg/gradient.jpg", "photo.jpg")

	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"convert", "ok.heic"}, 0},
		{[]string{"convert", "ok.heic"}, 0}, // re-run: skipped, still success
		{[]string{"convert", "bad.heic"}, 1},
		{[]string{"convert", "--quality", "500", "ok.heic"}, 2},
		{[]string{"convert", "photo.jpg"}, 4},
	} {
		if r := fmtshift(t, dir, tc.args...); r.code != tc.code {
			t.Errorf("%v: exit %d; want %d\n%+v", tc.args, r.code, tc.code, r)
		}
	}
}

// SIGINT stops a batch: exit 130, nothing partial left behind. Needs inputs
// slow enough to still be converting when the signal arrives, so it uses the
// 12 MP benchmark images (make bench) and skips without them.
func TestInterruptStopsBatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no SIGINT on Windows")
	}
	big := filepath.Join(fixtures, "bench", "12mp.heic")
	if _, err := os.Stat(big); err != nil {
		t.Skip("benchmark images missing; run `make bench` once")
	}
	dir := t.TempDir()
	var args []string
	for i := range 8 {
		name := fmt.Sprintf("p%d.heic", i)
		copyInto(t, dir, "bench/12mp.heic", name)
		args = append(args, name)
	}

	cmd := exec.Command(binary, append([]string{"convert", "--to", "png"}, args...)...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	cmd.Process.Signal(syscall.SIGINT)
	err := cmd.Wait()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 130 {
		t.Fatalf("exit = %v; want 130\nstderr: %s", err, stderr.String())
	}
	entries, _ := os.ReadDir(dir)
	var pngs, temps int
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), ".fmtshift-tmp"):
			temps++
		case strings.HasSuffix(e.Name(), ".png"):
			pngs++
		}
	}
	if temps != 0 {
		t.Errorf("%d temporary files left behind", temps)
	}
	if pngs >= len(args) {
		t.Errorf("all %d files converted; the interrupt had no effect", pngs)
	}
}
