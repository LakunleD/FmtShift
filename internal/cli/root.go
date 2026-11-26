// Package cli implements the fmtshift command-line interface. It parses
// arguments, calls the application services and maps outcomes to output and
// exit codes; it contains no image-processing logic.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/lakunled/fmtshift/internal/app"
)

// Exit codes, as specified in docs/CLI.md.
const (
	ExitOK          = 0
	ExitFailed      = 1   // one or more conversions failed
	ExitUsage       = 2   // invalid command or arguments
	ExitConfig      = 3   // configuration error (from M3)
	ExitNoEligible  = 4   // no eligible input files
	ExitInterrupted = 130 // interrupted by the user
)

// exitError carries an exit code from a command to Run. A nil err means the
// command already reported everything it needed to.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("exit %d", e.code)
	}
	return e.err.Error()
}

func usageError(format string, args ...any) error {
	return &exitError{code: ExitUsage, err: fmt.Errorf(format, args...)}
}

// env is shared by all commands of one invocation.
type env struct {
	stdout, stderr io.Writer
	newApp         func() (*app.App, error)
	verbose, quiet bool
}

// infof writes progress output (per-file results and summaries), which
// --quiet suppresses. Output a command exists to produce, such as the
// formats table or version, is written directly and is not affected.
func (e *env) infof(format string, args ...any) {
	if !e.quiet {
		fmt.Fprintf(e.stdout, format, args...)
	}
}

// Run executes fmtshift with args (excluding the program name) and returns
// the process exit code. newApp constructs the services; tests substitute a
// fake codec.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, newApp func() (*app.App, error)) int {
	e := &env{stdout: stdout, stderr: stderr, newApp: newApp}

	root := &cobra.Command{
		Use:   "fmtshift",
		Short: "Convert images locally, e.g. HEIC to JPEG",
		Long: "FmtShift converts images between formats on your own device.\n" +
			"Nothing is uploaded, and original files are never modified.",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			if e.verbose && e.quiet {
				return usageError("--verbose and --quiet cannot be used together")
			}
			return nil
		},
	}
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().BoolVarP(&e.verbose, "verbose", "v", false, "show detailed errors and timings")
	root.PersistentFlags().BoolVar(&e.quiet, "quiet", false, "hide per-file and summary messages; errors still print")

	root.AddCommand(newConvertCmd(e), newConfigCmd(e), newFormatsCmd(e), newVersionCmd(e))

	err := root.ExecuteContext(ctx)
	if err == nil {
		return ExitOK
	}
	var ee *exitError
	if errors.As(err, &ee) {
		if ee.err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", ee.err)
			if ee.code == ExitUsage {
				fmt.Fprintln(stderr, "Run 'fmtshift --help' for usage.")
			}
		}
		return ee.code
	}
	// Anything else comes from Cobra's own parsing: unknown commands or
	// flags, wrong argument counts.
	fmt.Fprintf(stderr, "Error: %v\nRun 'fmtshift --help' for usage.\n", err)
	return ExitUsage
}
