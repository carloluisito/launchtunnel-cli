package tunnel

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"nhooyr.io/websocket"
)

const (
	initialBackoff = 1 * time.Second
	maxBackoff     = 30 * time.Second
	maxAttempts    = 10
)

// ReconnectResult describes the outcome of a reconnection attempt.
type ReconnectResult struct {
	Conn *websocket.Conn
	Err  error
}

// Reconnect attempts to re-establish a WebSocket connection with exponential
// backoff. It returns the new connection on success or an error after
// maxAttempts failures.
func Reconnect(ctx context.Context, endpoint string, sessionToken string, verbose bool) (*websocket.Conn, error) {
	out := io.Writer(os.Stderr)

	backoff := initialBackoff
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if verbose {
			fmt.Fprintf(out, "Reconnection attempt %d/%d (waiting %s)...\n", attempt, maxAttempts, backoff)
		} else if attempt == 1 {
			fmt.Fprintln(out, "Connection lost. Reconnecting...")
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}

		conn, err := dialRelay(ctx, endpoint, sessionToken)
		if err == nil {
			fmt.Fprintln(out, "Reconnected successfully.")
			return conn, nil
		}

		if verbose {
			fmt.Fprintf(out, "Attempt %d failed: %v\n", attempt, err)
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	return nil, fmt.Errorf("unable to reconnect after %d attempts", maxAttempts)
}

// dialRelay establishes a WebSocket connection to the relay endpoint.
func dialRelay(ctx context.Context, endpoint string, sessionToken string) (*websocket.Conn, error) {
	sep := "?"
	if strings.Contains(endpoint, "?") {
		sep = "&"
	}
	wsURL := endpoint + sep + "session_token=" + sessionToken

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{})
	if err != nil {
		return nil, fmt.Errorf("dialing relay: %w", err)
	}
	// Increase read limit to support 10 MB payloads.
	conn.SetReadLimit(11 * 1024 * 1024)
	return conn, nil
}
