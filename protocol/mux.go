package protocol

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	"nhooyr.io/websocket"
)

var (
	ErrMuxClosed      = errors.New("protocol: mux closed")
	ErrStreamExists   = errors.New("protocol: stream already exists")
	ErrUnknownStream  = errors.New("protocol: unknown stream")
	ErrTooManyStreams = errors.New("protocol: too many concurrent streams")
)

// Mux multiplexes many logical streams over a single WebSocket connection.
type Mux struct {
	conn *websocket.Conn

	streams    map[uint32]*Stream
	mu         sync.RWMutex
	nextID     uint32 // odd for client, even for server
	isServer   bool
	maxStreams int // 0 means unlimited

	acceptCh chan *Stream

	onPong   func()
	onPongMu sync.RWMutex

	closed chan struct{}
	once   sync.Once
	done   chan struct{} // signalled when readLoop exits

	// writeCh is an async channel for outbound WebSocket frames.
	// A dedicated writeLoop goroutine drains it, removing per-stream
	// serialization through a mutex and preventing large payloads from
	// blocking small control frames.
	writeCh   chan []byte
	writeDone chan struct{} // closed when writeLoop exits
}

// NewMux creates a new multiplexer over conn.
// If isServer is true the mux allocates even stream IDs; otherwise odd.
// The caller should consume streams via AcceptStream.
func NewMux(conn *websocket.Conn, isServer bool) *Mux {
	m := &Mux{
		conn:      conn,
		streams:   make(map[uint32]*Stream),
		isServer:  isServer,
		acceptCh:  make(chan *Stream, 32),
		closed:    make(chan struct{}),
		done:      make(chan struct{}),
		writeCh:   make(chan []byte, 256),
		writeDone: make(chan struct{}),
	}
	if isServer {
		m.nextID = 2
	} else {
		m.nextID = 1
	}
	go m.readLoop()
	go m.writeLoop()
	return m
}

// SetMaxStreams sets the maximum number of concurrent streams.
// A value of 0 means unlimited.
func (m *Mux) SetMaxStreams(n int) {
	m.mu.Lock()
	m.maxStreams = n
	m.mu.Unlock()
}

// OpenStream creates a new outbound stream.
func (m *Mux) OpenStream(ctx context.Context) (*Stream, error) {
	select {
	case <-m.closed:
		return nil, ErrMuxClosed
	default:
	}

	m.mu.Lock()
	if m.maxStreams > 0 && len(m.streams) >= m.maxStreams {
		m.mu.Unlock()
		return nil, ErrTooManyStreams
	}
	id := m.nextID
	m.nextID += 2
	m.mu.Unlock()

	s := newStream(id, m.makeWriteFn(id), m.makeCloseFn(id))

	m.mu.Lock()
	m.streams[id] = s
	m.mu.Unlock()

	frame := EncodeFrame(Frame{Type: FrameOpenStream, StreamID: id})
	if err := m.writeWS(ctx, frame); err != nil {
		m.removeStream(id)
		return nil, fmt.Errorf("protocol: opening stream %d: %w", id, err)
	}

	return s, nil
}

// AcceptStream blocks until the remote side opens a stream or the mux is closed.
func (m *Mux) AcceptStream(ctx context.Context) (*Stream, error) {
	select {
	case s, ok := <-m.acceptCh:
		if !ok {
			return nil, ErrMuxClosed
		}
		return s, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.closed:
		return nil, ErrMuxClosed
	}
}

// SendPing sends a PING frame.
func (m *Mux) SendPing(ctx context.Context) error {
	select {
	case <-m.closed:
		return ErrMuxClosed
	default:
	}
	frame := EncodeFrame(Frame{Type: FramePing})
	return m.writeWS(ctx, frame)
}

// OnPong registers a callback that fires when a PONG frame is received.
func (m *Mux) OnPong(fn func()) {
	m.onPongMu.Lock()
	m.onPong = fn
	m.onPongMu.Unlock()
}

// Done returns a channel that is closed when the mux's readLoop exits.
// This can be used to detect when the underlying WebSocket connection broke.
func (m *Mux) Done() <-chan struct{} {
	return m.done
}

// Close shuts down the mux: closes all streams, the accept channel, and the
// underlying WebSocket connection. It waits for the readLoop to exit.
func (m *Mux) Close() error {
	m.shutdown()
	<-m.done
	return nil
}

// shutdown performs the one-time teardown logic without waiting for readLoop.
func (m *Mux) shutdown() {
	m.once.Do(func() {
		close(m.closed)

		m.mu.Lock()
		for _, s := range m.streams {
			s.closeRead()
		}
		m.streams = make(map[uint32]*Stream)
		m.mu.Unlock()

		close(m.acceptCh)

		// Stop the writeLoop and wait for it to drain.
		close(m.writeCh)
		<-m.writeDone

		// Close the websocket; this will cause readLoop to exit.
		m.conn.Close(websocket.StatusNormalClosure, "mux closed")
	})
}

// readLoop reads frames from the WebSocket and dispatches them.
func (m *Mux) readLoop() {
	defer close(m.done)

	for {
		_, data, err := m.conn.Read(context.Background())
		if err != nil {
			// Connection closed or broken — trigger shutdown (non-blocking).
			m.shutdown()
			return
		}

		if len(data) < frameHeaderSize {
			continue
		}

		f, err := DecodeFrame(bytes.NewReader(data))
		if err != nil {
			continue
		}

		select {
		case <-m.closed:
			return
		default:
		}

		switch f.Type {
		case FrameOpenStream:
			m.handleOpenStream(f.StreamID)
		case FrameData:
			m.handleData(f.StreamID, f.Payload)
		case FrameCloseStream:
			m.handleCloseStream(f.StreamID)
		case FramePing:
			m.handlePing()
		case FramePong:
			m.handlePong()
		}
	}
}

func (m *Mux) handleOpenStream(id uint32) {
	s := newStream(id, m.makeWriteFn(id), m.makeCloseFn(id))

	m.mu.Lock()
	m.streams[id] = s
	m.mu.Unlock()

	select {
	case m.acceptCh <- s:
	case <-m.closed:
	}
}

func (m *Mux) handleData(id uint32, payload []byte) {
	m.mu.RLock()
	s, ok := m.streams[id]
	m.mu.RUnlock()
	if !ok {
		return
	}
	s.pushData(payload)
}

func (m *Mux) handleCloseStream(id uint32) {
	m.mu.RLock()
	s, ok := m.streams[id]
	m.mu.RUnlock()
	if !ok {
		return
	}
	s.closeRead()
	m.removeStream(id)
}

func (m *Mux) handlePing() {
	frame := EncodeFrame(Frame{Type: FramePong})
	_ = m.writeWS(context.Background(), frame)
}

func (m *Mux) handlePong() {
	m.onPongMu.RLock()
	fn := m.onPong
	m.onPongMu.RUnlock()
	if fn != nil {
		fn()
	}
}

// writeLoop is a dedicated goroutine that drains writeCh and sends frames
// over the WebSocket connection. It exits when writeCh is closed.
func (m *Mux) writeLoop() {
	defer close(m.writeDone)
	for data := range m.writeCh {
		if err := m.conn.Write(context.Background(), websocket.MessageBinary, data); err != nil {
			m.shutdown()
			return
		}
	}
}

// writeWS enqueues a raw frame for the writeLoop goroutine.
// Returns immediately unless the write channel is full, in which case
// it blocks until space is available or the mux is closed.
func (m *Mux) writeWS(_ context.Context, data []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = ErrMuxClosed
		}
	}()

	select {
	case m.writeCh <- data:
		return nil
	case <-m.closed:
		return ErrMuxClosed
	}
}

func (m *Mux) makeWriteFn(id uint32) func([]byte) error {
	return func(payload []byte) error {
		select {
		case <-m.closed:
			return ErrMuxClosed
		default:
		}
		frame := EncodeFrame(Frame{Type: FrameData, StreamID: id, Payload: payload})
		return m.writeWS(context.Background(), frame)
	}
}

func (m *Mux) makeCloseFn(id uint32) func() {
	return func() {
		frame := EncodeFrame(Frame{Type: FrameCloseStream, StreamID: id})
		_ = m.writeWS(context.Background(), frame)
		m.removeStream(id)
	}
}

func (m *Mux) removeStream(id uint32) {
	m.mu.Lock()
	delete(m.streams, id)
	m.mu.Unlock()
}
