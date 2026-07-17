// Command valiss is the command-line tool for operating a valiss trust
// domain: minting operator, account, and user tokens, managing keys and
// creds files, and revocation.
//
// This is a pre-release scaffold. The command tree specified by ADR 0021 is
// wired on cobra and viper (ADR 0019), but every command body is a stub
// returning a not-implemented error; the bodies land with the store layer
// (ADR 0020).
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// version is the CLI version. Release builds override it via
// -ldflags "-X main.version=...".
var version = "0.0.0-dev"

func main() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newRootCommand builds the valiss root command and its noun-verb tree.
// The tree is specified by ADR 0021; entities are addressed by explicit
// paths with no hidden current-context.
func newRootCommand() *cobra.Command {
	var cfgFile string

	cmd := &cobra.Command{
		Use:     "valiss",
		Short:   "valiss trust-domain management CLI",
		Version: version,
		// Usage on an error is noise once flags parse; report the error only.
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfig(cfgFile)
		},
	}

	cmd.PersistentFlags().StringVar(&cfgFile, "config", "",
		"config file (default is $HOME/.valiss/config.yaml)")

	cmd.AddCommand(
		newOperatorCommand(),
		newAccountCommand(),
		newUserCommand(),
		newTemplateCommand(),
		newTokenCommand(),
		newCredsCommand(),
		newAllowlistCommand(),
		newStoreCommand(),
		newInspectCommand(),
	)

	return cmd
}

// initConfig binds configuration from the ~/.valiss dot-dir and VALISS_*
// environment variables (the conventions fixed in ADR 0017), using viper
// per ADR 0019.
func initConfig(cfgFile string) error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("valiss: locating home directory: %w", err)
		}
		viper.AddConfigPath(filepath.Join(home, ".valiss"))
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}

	viper.SetEnvPrefix("VALISS")
	// Map hyphenated/dotted config keys to underscore-delimited environment
	// variables, so store-dir binds to VALISS_STORE_DIR.
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	viper.AutomaticEnv()

	// A missing config file is not an error: the CLI runs on flags and
	// environment alone until one exists.
	var notFound viper.ConfigFileNotFoundError
	if err := viper.ReadInConfig(); err != nil && !errors.As(err, &notFound) {
		return fmt.Errorf("valiss: reading config: %w", err)
	}
	return nil
}
