package cmd

import (
	"fmt"
	"os"

	"github.com/carloluisito/launchtunnel-cli/config"
	"github.com/spf13/cobra"
)

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.RemoveCredentials(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Println("Logged out. Credentials removed.")
			return nil
		},
	}
}
