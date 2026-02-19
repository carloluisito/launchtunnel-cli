package cmd

import (
	"fmt"
	"os"

	"github.com/carloluisito/launchtunnel-cli/config"
	"github.com/spf13/cobra"
)

var (
	// Set via ldflags at build time.
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Flags shared across all commands.
var (
	flagConfigPath string
	flagAPIURL     string
	flagVerbose    bool
	flagNoColor    bool
)

// cliCfg is loaded once by the persistent pre-run hook.
var cliCfg config.CLIConfig

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "lt",
		Short:         "LaunchTunnel - share what you're building with the world",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, err := config.ConfigPath(flagConfigPath)
			if err != nil {
				return err
			}
			cliCfg, err = config.LoadCLIConfig(cfgPath)
			if err != nil {
				return err
			}
			// Flag > env > credentials file > config file.
			if flagAPIURL != "" {
				cliCfg.APIURL = flagAPIURL
			} else if env := os.Getenv("LT_API_URL"); env != "" {
				cliCfg.APIURL = env
			} else if creds, _ := config.LoadCredentials(); creds != nil && creds.APIURL != "" {
				cliCfg.APIURL = creds.APIURL
			}
			return nil
		},
	}

	root.PersistentFlags().StringVar(&flagConfigPath, "config", "", "path to config file (default: ~/.launchtunnel/config.json)")
	root.PersistentFlags().StringVar(&flagAPIURL, "api-url", "", "override the control plane API URL")
	root.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, "enable verbose/debug logging to stderr")
	root.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "disable colored output")

	root.AddCommand(
		newPreviewCmd(),
		newExposeCmd(),
		newListCmd(),
		newStopCmd(),
		newStatusCmd(),
		newVersionCmd(),
		newLoginCmd(),
		newLogoutCmd(),
		newSignupCmd(),
		newAPIKeyCmd(),
	)

	return root
}

// Execute runs the root command and exits with the appropriate code.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// requireAuth loads credentials and returns the API key, or prints an error and
// returns an empty string.
func requireAuth() (string, error) {
	creds, err := config.LoadCredentials()
	if err != nil {
		return "", fmt.Errorf("reading credentials: %w", err)
	}
	if creds == nil || creds.APIKey == "" {
		return "", fmt.Errorf("Not authenticated. Run 'lt login' first.")
	}
	return creds.APIKey, nil
}
