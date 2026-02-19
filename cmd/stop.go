package cmd

import (
	"fmt"
	"os"

	"github.com/carloluisito/launchtunnel-cli/client"
	"github.com/spf13/cobra"
)

func newStopCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "stop [tunnel_id]",
		Short: "Stop one or all active tunnels",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !all && len(args) == 0 {
				fmt.Fprintln(os.Stderr, "Provide a tunnel ID or use --all to stop all tunnels.")
				os.Exit(1)
			}

			apiKey, err := requireAuth()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			c := client.New(cliCfg.APIURL, apiKey)

			if all {
				tunnels, err := c.ListTunnels()
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
				count := 0
				for _, t := range tunnels {
					if err := c.DeleteTunnel(t.ID); err != nil {
						fmt.Fprintf(os.Stderr, "Failed to stop %s: %v\n", t.ID, err)
						continue
					}
					count++
				}
				fmt.Printf("Stopped %d tunnel(s).\n", count)
				return nil
			}

			tunnelID := args[0]
			if err := c.DeleteTunnel(tunnelID); err != nil {
				if apiErr, ok := err.(*client.APIError); ok && apiErr.HTTPStatus == 404 {
					fmt.Fprintf(os.Stderr, "Tunnel %s not found.\n", tunnelID)
					os.Exit(1)
				}
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			fmt.Printf("Tunnel %s stopped.\n", tunnelID)
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "stop all active tunnels")
	return cmd
}
