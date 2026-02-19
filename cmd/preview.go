package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/carloluisito/launchtunnel-cli/client"
	"github.com/carloluisito/launchtunnel-cli/display"
	"github.com/spf13/cobra"
)

func newPreviewCmd() *cobra.Command {
	var (
		port        int
		name        string
		project     string
		protocol    string
		expires     string
		authMode    string
		ipAllow     string
		subdomain   string
		localHost   string
		inspect     bool
		jsonOutput  bool
		noReconnect bool
		description string
		branch      string
	)

	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Create a shareable preview environment for a local application",
		Long: `Create a shareable preview environment for a local application.

This is the recommended way to create previews. Use 'lt expose' for
backward-compatible tunnel creation.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if port == 0 {
				fmt.Fprintln(os.Stderr, "Error: --port is required")
				os.Exit(1)
			}
			if port < 1 || port > 65535 {
				fmt.Fprintln(os.Stderr, "Invalid port number. Port must be between 1 and 65535.")
				os.Exit(1)
			}

			proto := strings.ToLower(protocol)
			if proto != "http" && proto != "tcp" {
				fmt.Fprintln(os.Stderr, "Invalid protocol. Must be 'http' or 'tcp'.")
				os.Exit(1)
			}

			// Normalize "d" suffix to hours for Go's time.ParseDuration.
			if expires != "" && strings.HasSuffix(expires, "d") {
				daysStr := strings.TrimSuffix(expires, "d")
				days, err := strconv.Atoi(daysStr)
				if err != nil || days <= 0 {
					fmt.Fprintln(os.Stderr, "Invalid --expires value. Use formats like: 1h, 4h, 8h, 24h, 48h, 7d")
					os.Exit(1)
				}
				expires = strconv.Itoa(days*24) + "h"
			}

			apiKey, err := requireAuth()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			if localHost == "" {
				localHost = cliCfg.DefaultLocalHost
			}

			c := client.New(cliCfg.APIURL, apiKey)

			tun, err := c.CreateTunnel(client.CreateTunnelRequest{
				Protocol:    proto,
				LocalPort:   port,
				LocalHost:   localHost,
				Name:        name,
				Subdomain:   subdomain,
				Description: description,
				Branch:      branch,
				ExpiresIn:   expires,
			})
			if err != nil {
				if apiErr, ok := err.(*client.APIError); ok {
					fmt.Fprintln(os.Stderr, apiErr.Message)
					os.Exit(1)
				}
				fmt.Fprintln(os.Stderr, "Unable to reach LaunchTunnel servers. Check your internet connection.")
				os.Exit(1)
			}

			// Set password if --auth was provided.
			if authMode != "" {
				if err := c.SetTunnelPassword(tun.ID, authMode); err != nil {
					if apiErr, ok := err.(*client.APIError); ok {
						fmt.Fprintln(os.Stderr, apiErr.Message)
						os.Exit(1)
					}
					fmt.Fprintln(os.Stderr, "Failed to set tunnel password.")
					os.Exit(1)
				}
			}

			// Set IP allowlist if --ip-allow was provided.
			if ipAllow != "" {
				ips := strings.Split(ipAllow, ",")
				for i := range ips {
					ips[i] = strings.TrimSpace(ips[i])
				}
				if err := c.SetTunnelIPAllowlist(tun.ID, ips); err != nil {
					if apiErr, ok := err.(*client.APIError); ok {
						fmt.Fprintln(os.Stderr, apiErr.Message)
						os.Exit(1)
					}
					fmt.Fprintln(os.Stderr, "Failed to set IP allowlist.")
					os.Exit(1)
				}
			}

			if jsonOutput {
				display.PrintJSON(os.Stdout, map[string]any{
					"preview_id": tun.ID,
					"name":       tun.Name,
					"public_url": tun.PublicURL,
					"protocol":   tun.Protocol,
					"local_host": localHost,
					"local_port": port,
					"status":     tun.Status,
					"created_at": tun.CreatedAt.Format(time.RFC3339),
				})
			} else {
				fmt.Println()
				fmt.Println("  Preview is live!")
				fmt.Println()
				fmt.Printf("    URL:        %s\n", tun.PublicURL)
				fmt.Printf("    Name:       %s\n", tun.Name)
				if project != "" {
					fmt.Printf("    Project:    %s\n", project)
				}
				fmt.Printf("    Protocol:   %s\n", tun.Protocol)
				fmt.Printf("    Local:      %s:%d\n", localHost, port)
				if tun.ExpiresAt != nil {
					fmt.Printf("    Expires:    %s\n", formatDuration(time.Until(*tun.ExpiresAt)))
				}
				fmt.Printf("    Preview ID: %s\n", tun.ID)
				fmt.Println()
			}

			// Connect to the relay.
			conn, err := dialRelay(tun.RelayEndpoint, tun.SessionToken)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to connect to relay: %v\n", err)
				os.Exit(2)
			}

			if !jsonOutput {
				fmt.Println("  Press Ctrl+C to stop.")
				fmt.Println()
			}

			return runTunnelLoop(conn, tun, localHost, port, proto, inspect, noReconnect, c)
		},
	}

	cmd.Flags().IntVar(&port, "port", 0, "local port to expose (required)")
	cmd.Flags().StringVar(&name, "name", "", "preview name (alphanumeric + hyphens, 3-63 chars)")
	cmd.Flags().StringVar(&project, "project", "", "assign to a project (default: personal)")
	cmd.Flags().StringVar(&protocol, "protocol", "http", "protocol: http or tcp")
	cmd.Flags().StringVar(&expires, "expires", "", "auto-expire: 1h, 4h, 8h, 24h, 48h, 7d")
	cmd.Flags().StringVar(&authMode, "auth", "", "access control: password")
	cmd.Flags().StringVar(&ipAllow, "ip-allow", "", "comma-separated IP/CIDR allowlist")
	cmd.Flags().StringVar(&subdomain, "subdomain", "", "custom subdomain (Pro only)")
	cmd.Flags().StringVar(&localHost, "local-host", "", "local hostname to forward to (default: 127.0.0.1)")
	cmd.Flags().BoolVar(&inspect, "inspect", false, "enable request logging")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&noReconnect, "no-reconnect", false, "disable automatic reconnection")
	cmd.Flags().StringVar(&description, "description", "", "preview description")
	cmd.Flags().StringVar(&branch, "branch", "", "git branch name")

	_ = cmd.MarkFlagRequired("port")

	return cmd
}

// formatDuration formats a duration into a human-readable string.
func formatDuration(d time.Duration) string {
	if d < 0 {
		return "expired"
	}
	hours := int(d.Hours())
	if hours < 1 {
		minutes := int(d.Minutes())
		return "in " + strconv.Itoa(minutes) + " minutes"
	}
	if hours < 24 {
		return "in " + strconv.Itoa(hours) + " hours"
	}
	days := hours / 24
	return "in " + strconv.Itoa(days) + " days"
}
