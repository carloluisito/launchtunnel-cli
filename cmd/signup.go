package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newSignupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "signup",
		Short: "Create a new LaunchTunnel account",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			signupURL := cliCfg.FrontendURL + "/signup"
			fmt.Println("Check your browser to complete signup.")
			fmt.Printf("Signup URL: %s\n", signupURL)
			openBrowser(signupURL)
		},
	}
}
