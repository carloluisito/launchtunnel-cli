package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/carloluisito/launchtunnel-cli/protocol"
)

const localDialTimeout = 5 * time.Second

// Stderr is the writer used for warnings and inspect output.
// It defaults to os.Stderr but can be replaced for testing.
var Stderr io.Writer = os.Stderr

// transportCache pools HTTP transports by target address so connections are
// reused across requests (avoids a new TCP handshake per asset).
var (
	transportMu    sync.Mutex
	transportCache = make(map[string]*http.Transport)
)

func getTransport(target string) *http.Transport {
	transportMu.Lock()
	defer transportMu.Unlock()
	if t, ok := transportCache[target]; ok {
		return t
	}
	t := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
		DialContext: (&net.Dialer{
			Timeout: localDialTimeout,
		}).DialContext,
	}
	transportCache[target] = t
	return t
}

// ForwardHTTP reads an HTTP request from the stream, forwards it to the local
// server using a pooled connection, and writes the response back to the stream.
func ForwardHTTP(stream *protocol.Stream, localHost string, localPort int, inspect bool, verbose bool) {
	defer stream.Close()

	target := net.JoinHostPort(localHost, fmt.Sprintf("%d", localPort))

	req, err := http.ReadRequest(bufio.NewReader(stream))
	if err != nil {
		if verbose {
			fmt.Fprintf(Stderr, "error reading request from stream: %v\n", err)
		}
		return
	}

	// Prepare the request for RoundTrip (needs absolute URL, no RequestURI).
	req.URL.Scheme = "http"
	req.URL.Host = target
	req.RequestURI = ""

	start := time.Now()

	transport := getTransport(target)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		fmt.Fprintf(Stderr, "Warning: Connection to %s refused. Is your application running?\n", target)
		errResp := &http.Response{
			StatusCode: http.StatusBadGateway,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header:     make(http.Header),
			Body:       http.NoBody,
		}
		errResp.Header.Set("Content-Type", "application/json")
		_ = errResp.Write(stream)
		return
	}
	defer resp.Body.Close()

	duration := time.Since(start)

	if inspect {
		fmt.Fprintf(Stderr, "%s %s %d %s\n",
			req.Method, req.URL.Path, resp.StatusCode, duration.Truncate(time.Millisecond))
	}

	// Buffer response writes so all headers + start of body coalesce into
	// one or two large WebSocket DATA frames instead of many small ones.
	bw := bufio.NewWriterSize(stream, 65536)
	if err := resp.Write(bw); err != nil {
		if verbose {
			fmt.Fprintf(Stderr, "error writing response to stream: %v\n", err)
		}
		return
	}
	if err := bw.Flush(); err != nil {
		if verbose {
			fmt.Fprintf(Stderr, "error flushing response to stream: %v\n", err)
		}
	}
}

// ForwardTCP performs bidirectional byte copying between the stream and the
// local TCP server.
func ForwardTCP(stream *protocol.Stream, localHost string, localPort int, verbose bool) {
	defer stream.Close()

	target := net.JoinHostPort(localHost, fmt.Sprintf("%d", localPort))

	conn, err := net.DialTimeout("tcp", target, localDialTimeout)
	if err != nil {
		fmt.Fprintf(Stderr, "Warning: Connection to %s refused. Is your application running?\n", target)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		defer cancel()
		_, _ = io.Copy(stream, conn)
	}()

	go func() {
		defer cancel()
		_, _ = io.Copy(conn, stream)
	}()

	<-ctx.Done()
}
