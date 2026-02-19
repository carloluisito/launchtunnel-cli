package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/carloluisito/launchtunnel-cli/client"
	"github.com/carloluisito/launchtunnel-cli/display"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active tunnels",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			apiKey, err := requireAuth()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			c := client.New(cliCfg.APIURL, apiKey)
			tunnels, err := c.ListTunnels()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			if jsonOutput {
				return display.PrintJSON(os.Stdout, tunnels)
			}

			if len(tunnels) == 0 {
				fmt.Println("No active tunnels.")
				return nil
			}

			tbl := display.NewTable("ID", "URL", "PROTOCOL", "LOCAL", "STATUS", "AGE")
			for _, t := range tunnels {
				local := fmt.Sprintf("%s:%d", t.LocalHost, t.LocalPort)
				age := formatAge(t.CreatedAt)
				tbl.AddRow(t.ID, t.PublicURL, t.Protocol, local, t.Status, age)
			}
			tbl.Render(os.Stdout)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON array")
	return cmd
}

func formatAge(created time.Time) string {
	d := time.Since(created)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		days := int(d.Hours()) / 24
		return fmt.Sprintf("%dd", days)
	}
}
