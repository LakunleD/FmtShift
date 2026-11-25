// Command fmtshift converts images locally. See docs/CLI.md.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/lakunled/fmtshift/internal/app"
	"github.com/lakunled/fmtshift/internal/cli"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The first Ctrl+C (or SIGTERM) cancels: the current file finishes
	// (libvips cannot be interrupted mid-image) and nothing further starts.
	// Default handling is then restored, so a second Ctrl+C quits at once.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		signal.Stop(sigs)
		fmt.Fprintln(os.Stderr, "\nStopping after the current file (press Ctrl+C again to quit now)...")
		cancel()
	}()

	return cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr, app.New)
}
