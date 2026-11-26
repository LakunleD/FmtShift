package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lakunled/fmtshift/internal/config"
)

func newConfigCmd(e *env) *cobra.Command {
	var keys strings.Builder
	for _, k := range config.Keys {
		fmt.Fprintf(&keys, "  %-15s %s\n", k.Name, k.Description)
	}
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage saved default settings",
		Long: "Save default settings for convert. Options given on the command line\n" +
			"always take precedence over saved settings, which take precedence over\n" +
			"built-in defaults.\n\n" +
			"Keys:\n" + keys.String() + "\n" +
			"Overwriting existing files cannot be saved as a default; it always needs\n" +
			"--overwrite on the command. The file location can be changed with the\n" +
			config.EnvPath + " environment variable.",
		Example: "  fmtshift config set format png\n" +
			"  fmtshift config set quality 90\n" +
			"  fmtshift config set output ~/Pictures/Converted\n" +
			"  fmtshift config show",
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "set <key> <value>",
			Short: "Save a default setting",
			Args:  cobra.ExactArgs(2),
			RunE: func(_ *cobra.Command, args []string) error {
				key, err := config.LookupKey(args[0])
				if err != nil {
					return usageError("%v", unwrapInvalid(err))
				}
				// Validate before touching the file, so a bad value is a usage
				// error even when the file itself is fine.
				if err := key.Set(&config.Config{}, args[1]); err != nil {
					return usageError("%v", unwrapInvalid(err))
				}
				path, err := config.Path()
				if err != nil {
					return configError(err)
				}
				if err := config.Update(path, func(c *config.Config) error { return key.Set(c, args[1]) }); err != nil {
					return configError(err)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "get <key>",
			Short: "Print the setting in effect for a key",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				key, err := config.LookupKey(args[0])
				if err != nil {
					return usageError("%v", unwrapInvalid(err))
				}
				cfg, _, err := loadConfig()
				if err != nil {
					return err
				}
				fmt.Fprintln(e.stdout, key.Get(cfg.Effective()))
				return nil
			},
		},
		&cobra.Command{
			Use:   "show",
			Short: "Print all settings in effect, as JSON",
			Args:  cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				cfg, _, err := loadConfig()
				if err != nil {
					return err
				}
				out, err := json.MarshalIndent(cfg.Effective(), "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(e.stdout, string(out))
				return nil
			},
		},
		&cobra.Command{
			Use:   "path",
			Short: "Print the location of the configuration file",
			Args:  cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				path, err := config.Path()
				if err != nil {
					return configError(err)
				}
				fmt.Fprintln(e.stdout, path)
				return nil
			},
		},
	)
	return cmd
}

// loadConfig reads the saved configuration. Problems are configuration
// errors (exit 3); an invalid file is reported, never replaced.
func loadConfig() (config.Config, string, error) {
	path, err := config.Path()
	if err != nil {
		return config.Config{}, "", configError(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return config.Config{}, path, configError(err)
	}
	return cfg, path, nil
}

func configError(err error) error {
	msg := err.Error()
	if errors.Is(err, config.ErrInvalid) {
		msg += "\nFix or delete the file; it will not be changed automatically."
	}
	return &exitError{code: ExitConfig, err: errors.New(msg)}
}

// unwrapInvalid drops the "invalid configuration: " prefix for messages
// about command-line arguments, which are not about the file.
func unwrapInvalid(err error) string {
	return strings.TrimPrefix(err.Error(), config.ErrInvalid.Error()+": ")
}
