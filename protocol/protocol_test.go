package protocol

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

// ---------------------------------------------------------------------------
// Frame tests
// ---------------------------------------------------------------------------

func TestEncodeDecodeRoundtrip(t *testing.T) {
	cases := []struct {
		name  string
		frame Frame
	}{
		{
			name:  "open stream",
			frame: Frame{Type: FrameOpenStream, StreamID: 1},
		},
		{
			name:  "data with payload",
			frame: Frame{Type: FrameData, StreamID: 42, Payload: []byte("hello world")},
		},
		{
			name:  "close stream",
			frame: Frame{Type: FrameCloseStream, StreamID: 100},
		},
		{
			name:  "ping no payload",
			frame: Frame{Type: FramePing, StreamID: 0},
		},
		{
			name:  "pong no payload",
			frame: Frame{Type: FramePong, StreamID: 0},
		},
		{
			name:  "empty payload",
			frame: Frame{Type: FrameData, StreamID: 7, Payload: []byte{}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := EncodeFrame(tc.frame)
			decoded, err := DecodeFrame(bytes.NewReader(encoded))
			if err != nil {
				t.Fatalf("DecodeFrame: %v", err)
			}
			if decoded.Type != tc.frame.Type {
				t.Errorf("Type: got 0x%02x, want 0x%02x", decoded.Type, tc.frame.Type)
			}
			if decoded.StreamID != tc.frame.StreamID {
				t.Errorf("StreamID: got %d, want %d", decoded.StreamID, tc.frame.StreamID)
			}
			if !bytes.Equal(decoded.Payload, tc.frame.Payload) {
				t.Errorf("Payload: got %q, want %q", decoded.Payload, tc.frame.Payload)
			}
		})
	}
}

func TestDecodeFrame_InvalidType(t *testing.T) {
	f := Frame{Type: 0xFF, StreamID: 1}
	encoded := EncodeFrame(f)
	_, err := DecodeFrame(bytes.NewReader(encoded))
	if err == nil {
		t.Fatal("expected error for invalid frame type")
	}
}

func TestDecodeFrame_PayloadTooLarge(t *testing.T) {
	// Craft a header that claims a payload larger than MaxPayloadSize.
	hdr := make([]byte, frameHeaderSize)
	hdr[0] = FrameData
	// stream ID = 1
	hdr[1], hdr[2], hdr[3], hdr[4] = 0, 0, 0, 1
	// payload len = MaxPayloadSize + 1
	size := uint32(MaxPayloadSize + 1)
	hdr[5] = byte(size >> 24)
	hdr[6] = byte(size >> 16)
	hdr[7] = byte(size >> 8)
	hdr[8] = byte(size)

	_, err := DecodeFrame(bytes.NewReader(hdr))
	if err == nil {
		t.Fatal("expected error for payload too large")
	}
}

func TestDecodeFrame_ShortRead(t *testing.T) {
	_, err := DecodeFrame(bytes.NewReader([]byte{0x01, 0x02}))
	if err == nil {
		t.Fatal("expected error for short header")
	}
}

func TestEncodeFrame_HeaderSize(t *testing.T) {
	f := Frame{Type: FrameData, StreamID: 5, Payload: []byte("abc")}
	encoded := EncodeFrame(f)
	if len(encoded) != frameHeaderSize+3 {
		t.Fatalf("encoded length: got %d, want %d", len(encoded), frameHeaderSize+3)
	}
}

// ---------------------------------------------------------------------------
// Stream tests
// ---------------------------------------------------------------------------

func TestStream_ReadWrite(t *testing.T) {
	var written []byte
	var mu sync.Mutex

	writeFn := func(data []byte) error {
		mu.Lock()
		written = append(written, data...)
		mu.Unlock()
		return nil
	}

	s := newStream(1, writeFn, func() {})

	// Write data.
	msg := []byte("hello stream")
	n, err := s.Write(msg)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(msg) {
		t.Fatalf("Write n: got %d, want %d", n, len(msg))
	}

	mu.Lock()
	if !bytes.Equal(written, msg) {
		t.Errorf("writeFn received %q, want %q", written, msg)
	}
	mu.Unlock()

	// Push data and read it back.
	s.pushData([]byte("response"))
	buf := make([]byte, 64)
	n, err = s.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != "response" {
		t.Errorf("Read: got %q, want %q", buf[:n], "response")
	}
}

func TestStream_ReadAfterClose(t *testing.T) {
	s := newStream(1, func([]byte) error { return nil }, func() {})

	// Push some data, then close the read side.
	s.pushData([]byte("data"))
	s.closeRead()

	buf := make([]byte, 64)
	n, err := s.Read(buf)
	if err != nil {
		t.Fatalf("first Read after close should return buffered data: %v", err)
	}
	if string(buf[:n]) != "data" {
		t.Errorf("got %q, want %q", buf[:n], "data")
	}

	// Next read should return EOF.
	_, err = s.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestStream_WriteAfterClose(t *testing.T) {
	s := newStream(1, func([]byte) error { return nil }, func() {})
	s.Close()

	_, err := s.Write([]byte("data"))
	if err != ErrStreamClosed {
		t.Fatalf("expected ErrStreamClosed, got %v", err)
	}
}

func TestStream_PartialRead(t *testing.T) {
	s := newStream(1, func([]byte) error { return nil }, func() {})
	s.pushData([]byte("abcdef"))

	buf := make([]byte, 3)
	n, err := s.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != "abc" {
		t.Errorf("got %q, want %q", buf[:n], "abc")
	}

	// Read remaining.
	n, err = s.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != "def" {
		t.Errorf("got %q, want %q", buf[:n], "def")
	}
}

// ---------------------------------------------------------------------------
// Mux tests (using httptest + websocket)
// ---------------------------------------------------------------------------

// setupMuxPair creates a server/client mux pair connected via WebSocket.
func setupMuxPair(t *testing.T) (serverMux *Mux, clientMux *Mux, cleanup func()) {
	t.Helper()

	serverReady := make(chan *Mux, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("websocket.Accept: %v", err)
			return
		}
		m := NewMux(conn, true)
		serverReady <- m
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + srv.URL[len("http"):]
	clientConn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}

	clientM := NewMux(clientConn, false)

	select {
	case serverM := <-serverReady:
		return serverM, clientM, func() {
			clientM.Close()
			serverM.Close()
			srv.Close()
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server mux")
		return nil, nil, nil
	}
}

func TestMux_OpenAndAcceptStream(t *testing.T) {
	serverMux, clientMux, cleanup := setupMuxPair(t)
	defer cleanup()

	ctx := context.Background()

	// Client opens a stream.
	clientStream, err := clientMux.OpenStream(ctx)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}

	// Server accepts the stream.
	serverStream, err := serverMux.AcceptStream(ctx)
	if err != nil {
		t.Fatalf("AcceptStream: %v", err)
	}

	// Client stream ID should be odd.
	if clientStream.ID%2 != 1 {
		t.Errorf("client stream ID should be odd, got %d", clientStream.ID)
	}

	// Both sides should see the same stream ID.
	if serverStream.ID != clientStream.ID {
		t.Errorf("stream IDs mismatch: server=%d client=%d", serverStream.ID, clientStream.ID)
	}
}

func TestMux_StreamDataTransfer(t *testing.T) {
	serverMux, clientMux, cleanup := setupMuxPair(t)
	defer cleanup()

	ctx := context.Background()

	clientStream, err := clientMux.OpenStream(ctx)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	serverStream, err := serverMux.AcceptStream(ctx)
	if err != nil {
		t.Fatalf("AcceptStream: %v", err)
	}

	// Client writes, server reads.
	msg := []byte("hello from client")
	if _, err := clientStream.Write(msg); err != nil {
		t.Fatalf("Write: %v", err)
	}

	buf := make([]byte, 128)
	n, err := serverStream.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != "hello from client" {
		t.Errorf("got %q, want %q", buf[:n], "hello from client")
	}

	// Server writes back, client reads.
	reply := []byte("hello from server")
	if _, err := serverStream.Write(reply); err != nil {
		t.Fatalf("Write: %v", err)
	}

	n, err = clientStream.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != "hello from server" {
		t.Errorf("got %q, want %q", buf[:n], "hello from server")
	}
}

func TestMux_StreamClose(t *testing.T) {
	serverMux, clientMux, cleanup := setupMuxPair(t)
	defer cleanup()

	ctx := context.Background()

	clientStream, err := clientMux.OpenStream(ctx)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	serverStream, err := serverMux.AcceptStream(ctx)
	if err != nil {
		t.Fatalf("AcceptStream: %v", err)
	}

	// Client closes the stream.
	clientStream.Close()

	// Give the close frame time to propagate.
	time.Sleep(100 * time.Millisecond)

	// Server should get EOF.
	buf := make([]byte, 64)
	_, err = serverStream.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected io.EOF after remote close, got %v", err)
	}
}

func TestMux_ConcurrentStreams(t *testing.T) {
	serverMux, clientMux, cleanup := setupMuxPair(t)
	defer cleanup()

	ctx := context.Background()
	const numStreams = 10

	var wg sync.WaitGroup

	// Server goroutine accepts all streams and echoes data.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numStreams; i++ {
			s, err := serverMux.AcceptStream(ctx)
			if err != nil {
				t.Errorf("AcceptStream %d: %v", i, err)
				return
			}
			go func(s *Stream) {
				buf := make([]byte, 256)
				n, err := s.Read(buf)
				if err != nil {
					return
				}
				s.Write(buf[:n])
			}(s)
		}
	}()

	// Client opens N streams concurrently.
	clientStreams := make([]*Stream, numStreams)
	var openWg sync.WaitGroup
	for i := 0; i < numStreams; i++ {
		openWg.Add(1)
		go func(idx int) {
			defer openWg.Done()
			s, err := clientMux.OpenStream(ctx)
			if err != nil {
				t.Errorf("OpenStream %d: %v", idx, err)
				return
			}
			clientStreams[idx] = s
		}(i)
	}
	openWg.Wait()

	// Write to each stream and read back.
	for i, s := range clientStreams {
		if s == nil {
			continue
		}
		msg := []byte("ping")
		if _, err := s.Write(msg); err != nil {
			t.Errorf("stream %d Write: %v", i, err)
			continue
		}
		buf := make([]byte, 64)
		n, err := s.Read(buf)
		if err != nil {
			t.Errorf("stream %d Read: %v", i, err)
			continue
		}
		if string(buf[:n]) != "ping" {
			t.Errorf("stream %d: got %q, want %q", i, buf[:n], "ping")
		}
	}

	wg.Wait()
}

func TestMux_PingPong(t *testing.T) {
	serverMux, clientMux, cleanup := setupMuxPair(t)
	defer cleanup()

	pongReceived := make(chan struct{}, 1)
	clientMux.OnPong(func() {
		select {
		case pongReceived <- struct{}{}:
		default:
		}
	})

	ctx := context.Background()
	if err := clientMux.SendPing(ctx); err != nil {
		t.Fatalf("SendPing: %v", err)
	}

	select {
	case <-pongReceived:
		// success
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for pong")
	}

	_ = serverMux // keep reference
}

func TestMux_ServerOpenStream(t *testing.T) {
	serverMux, clientMux, cleanup := setupMuxPair(t)
	defer cleanup()

	ctx := context.Background()

	// Server opens a stream (even IDs).
	serverStream, err := serverMux.OpenStream(ctx)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}

	clientStream, err := clientMux.AcceptStream(ctx)
	if err != nil {
		t.Fatalf("AcceptStream: %v", err)
	}

	if serverStream.ID%2 != 0 {
		t.Errorf("server stream ID should be even, got %d", serverStream.ID)
	}
	if clientStream.ID != serverStream.ID {
		t.Errorf("stream ID mismatch: %d vs %d", clientStream.ID, serverStream.ID)
	}
}

func TestMux_CloseStopsAccept(t *testing.T) {
	serverMux, _, cleanup := setupMuxPair(t)
	defer cleanup()

	errCh := make(chan error, 1)
	go func() {
		_, err := serverMux.AcceptStream(context.Background())
		errCh <- err
	}()

	// Give AcceptStream time to block.
	time.Sleep(50 * time.Millisecond)
	serverMux.Close()

	select {
	case err := <-errCh:
		if err != ErrMuxClosed {
			t.Fatalf("expected ErrMuxClosed, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("AcceptStream did not unblock after Close")
	}
}

func TestMux_MultipleDataFrames(t *testing.T) {
	serverMux, clientMux, cleanup := setupMuxPair(t)
	defer cleanup()

	ctx := context.Background()

	cs, err := clientMux.OpenStream(ctx)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	ss, err := serverMux.AcceptStream(ctx)
	if err != nil {
		t.Fatalf("AcceptStream: %v", err)
	}

	// Send multiple chunks.
	for i := 0; i < 5; i++ {
		if _, err := cs.Write([]byte("chunk")); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	// Read all chunks.
	var total []byte
	buf := make([]byte, 64)
	for len(total) < 25 {
		n, err := ss.Read(buf)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		total = append(total, buf[:n]...)
	}

	if string(total) != "chunkchunkchunkchunkchunk" {
		t.Errorf("got %q", total)
	}
}
