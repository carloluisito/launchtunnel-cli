package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"nhooyr.io/websocket"

	"github.com/carloluisito/launchtunnel-cli/client"
	"github.com/carloluisito/launchtunnel-cli/display"
	"github.com/carloluisito/launchtunnel-cli/protocol"
	"github.com/carloluisito/launchtunnel-cli/tunnel"
	"github.com/spf13/cobra"
)

func newExposeCmd() *cobra.Command {
	var (
		name        string
		subdomain   string
		localHost   string
		inspect     bool
		noReconnect bool
		jsonOutput  bool
	)

	cmd := &cobra.Command{
		Use:   "expose <protocol> <port>",
		Short: "Expose a local port to the public internet",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			proto := strings.ToLower(args[0])
			if proto != "http" && proto != "tcp" {
				fmt.Fprintln(os.Stderr, "Invalid protocol. Must be 'http' or 'tcp'.")
				os.Exit(1)
			}

			port, err := strconv.Atoi(args[1])
			if err != nil || port < 1 || port > 65535 {
				fmt.Fprintln(os.Stderr, "Invalid port number. Port must be between 1 and 65535.")
				os.Exit(1)
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
				Protocol:  proto,
				LocalPort: port,
				LocalHost: localHost,
				Name:      name,
				Subdomain: subdomain,
			})
			if err != nil {
				if apiErr, ok := err.(*client.APIError); ok {
					fmt.Fprintln(os.Stderr, apiErr.Message)
					os.Exit(1)
				}
				fmt.Fprintln(os.Stderr, "Unable to reach LaunchTunnel servers. Check your internet connection.")
				os.Exit(1)
			}

			if jsonOutput {
				display.PrintJSON(os.Stdout, map[string]any{
					"tunnel_id":  tun.ID,
					"public_url": tun.PublicURL,
					"protocol":   tun.Protocol,
					"local_host": localHost,
					"local_port": port,
					"status":     tun.Status,
					"created_at": tun.CreatedAt.Format(time.RFC3339),
				})
			} else {
				fmt.Println("Tunnel established successfully.")
				fmt.Println()
				fmt.Printf("  Public URL:    %s\n", tun.PublicURL)
				fmt.Printf("  Protocol:      %s\n", tun.Protocol)
				fmt.Printf("  Local target:  %s:%d\n", localHost, port)
				fmt.Printf("  Tunnel ID:     %s\n", tun.ID)
				fmt.Printf("  Status:        %s\n", tun.Status)
				fmt.Println()
			}

			// Connect to the relay.
			conn, err := dialRelay(tun.RelayEndpoint, tun.SessionToken)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to connect to relay: %v\n", err)
				os.Exit(2)
			}

			if !jsonOutput {
				fmt.Println("Press Ctrl+C to stop the tunnel.")
			}

			return runTunnelLoop(conn, tun, localHost, port, proto, inspect, noReconnect, c)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "human-readable label for this tunnel")
	cmd.Flags().StringVar(&subdomain, "subdomain", "", "request a specific subdomain (Pro tier only)")
	cmd.Flags().StringVar(&localHost, "local-host", "", "local hostname to forward to (default: 127.0.0.1)")
	cmd.Flags().BoolVar(&inspect, "inspect", false, "enable request/response inspection logging (HTTP only)")
	cmd.Flags().BoolVar(&noReconnect, "no-reconnect", false, "disable automatic reconnection on disconnect")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output tunnel metadata as JSON")

	return cmd
}

func dialRelay(endpoint string, sessionToken string) (*websocket.Conn, error) {
	// The relay expects the session token as a query parameter.
	sep := "?"
	if strings.Contains(endpoint, "?") {
		sep = "&"
	}
	wsURL := endpoint + sep + "session_token=" + sessionToken

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{})
	if err != nil {
		return nil, fmt.Errorf("dialing relay: %w", err)
	}
	conn.SetReadLimit(11 * 1024 * 1024)
	return conn, nil
}

func runTunnelLoop(
	conn *websocket.Conn,
	tun *client.TunnelResponse,
	localHost string,
	localPort int,
	proto string,
	inspect bool,
	noReconnect bool,
	apiClient *client.Client,
) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	for {
		mux := protocol.NewMux(conn, false)

		// The relay sends pings; the mux automatically replies with pongs
		// via handlePing in readLoop. We just register a pong callback for
		// logging in verbose mode.
		if flagVerbose {
			mux.OnPong(func() {
				fmt.Fprintln(os.Stderr, "heartbeat: pong received")
			})
		}

		// Accept streams until mux closes or we are interrupted.
		exitCode := acceptStreams(ctx, mux, localHost, localPort, proto, inspect)

		if exitCode == 0 {
			// Tell the control plane we're stopping (best-effort).
			if apiClient != nil {
				_ = apiClient.StopTunnel(tun.ID)
			}
			conn.Close(websocket.StatusNormalClosure, "client shutdown")
			mux.Close()
			return nil
		}

		mux.Close()

		// Connection lost.
		if noReconnect || (cliCfg.AutoReconnect != nil && !*cliCfg.AutoReconnect) {
			fmt.Fprintln(os.Stderr, "Connection lost. Reconnection disabled.")
			os.Exit(2)
		}

		// Attempt reconnection.
		newConn, err := tunnel.Reconnect(ctx, tun.RelayEndpoint, tun.SessionToken, flagVerbose)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Unable to reconnect. Tunnel terminated.")
			os.Exit(2)
		}
		conn = newConn
	}
}

// acceptStreams accepts streams from the mux and forwards them.
// Returns 0 for graceful shutdown, 2 for connection loss.
func acceptStreams(ctx context.Context, mux *protocol.Mux, localHost string, localPort int, proto string, inspect bool) int {
	for {
		stream, err := mux.AcceptStream(ctx)
		if err != nil {
			// Check if it's a context cancellation (SIGINT).
			select {
			case <-ctx.Done():
				return 0
			default:
			}
			// Mux closed: connection lost.
			return 2
		}

		switch proto {
		case "http":
			go tunnel.ForwardHTTP(stream, localHost, localPort, inspect, flagVerbose)
		case "tcp":
			go tunnel.ForwardTCP(stream, localHost, localPort, flagVerbose)
		}
	}
}
