package cli

import (
	"fmt"
	"runtime"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/lakunled/fmtshift/internal/codecs"
	"github.com/lakunled/fmtshift/internal/format"
	"github.com/lakunled/fmtshift/internal/version"
)

func newFormatsCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "formats",
		Short: "List supported formats",
		Long:  "List the image formats this build can read and write.",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			a, err := e.newApp()
			if err != nil {
				return &exitError{code: ExitFailed, err: fmt.Errorf("cannot start the image library: %w", err)}
			}
			caps := a.Codec.Capabilities()
			tw := tabwriter.NewWriter(e.stdout, 0, 4, 4, ' ', 0)
			fmt.Fprintln(tw, "FORMAT\tREAD\tWRITE")
			for _, f := range format.All {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", f, yesNo(codecs.CanRead(caps, f)), yesNo(codecs.CanWrite(caps, f)))
			}
			return tw.Flush()
		},
	}
}

func newVersionCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			fmt.Fprintf(e.stdout, "FmtShift %s\n", version.String())
			fmt.Fprintf(e.stdout, "Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			// The version must print even if the image library is broken,
			// since that is exactly when users need to report it.
			if a, err := e.newApp(); err != nil {
				fmt.Fprintf(e.stdout, "Codec: unavailable (%v)\n", err)
			} else {
				fmt.Fprintf(e.stdout, "Codec: %s\n", a.Codec.Name())
			}
			return nil
		},
	}
}

func yesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}
