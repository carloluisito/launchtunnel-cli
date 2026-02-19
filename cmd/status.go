package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/carloluisito/launchtunnel-cli/client"
	"github.com/carloluisito/launchtunnel-cli/display"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status <tunnel_id>",
		Short: "Show the status of a specific tunnel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			apiKey, err := requireAuth()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			c := client.New(cliCfg.APIURL, apiKey)
			tun, err := c.GetTunnel(args[0])
			if err != nil {
				if apiErr, ok := err.(*client.APIError); ok && apiErr.HTTPStatus == 404 {
					fmt.Fprintf(os.Stderr, "Tunnel %s not found.\n", args[0])
					os.Exit(1)
				}
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			if jsonOutput {
				return display.PrintJSON(os.Stdout, tun)
			}

			fmt.Printf("Tunnel ID:       %s\n", tun.ID)
			fmt.Printf("Public URL:      %s\n", tun.PublicURL)
			fmt.Printf("Protocol:        %s\n", tun.Protocol)
			fmt.Printf("Local target:    %s:%d\n", tun.LocalHost, tun.LocalPort)
			fmt.Printf("Status:          %s\n", tun.Status)
			fmt.Printf("Uptime:          %s\n", formatUptime(tun.CreatedAt))
			fmt.Printf("Bytes in:        %s\n", display.FormatBytes(tun.BytesIn))
			fmt.Printf("Bytes out:       %s\n", display.FormatBytes(tun.BytesOut))
			fmt.Printf("Requests:        %d\n", tun.RequestCount)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func formatUptime(created time.Time) string {
	d := time.Since(created)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
