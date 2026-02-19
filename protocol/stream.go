package protocol

import (
	"errors"
	"io"
	"sync"
)

var (
	ErrStreamClosed = errors.New("protocol: stream closed")
)

// Stream implements io.ReadWriteCloser over a multiplexed connection.
// It is safe for concurrent use by multiple goroutines.
type Stream struct {
	ID uint32

	// dataCh carries incoming data chunks. readBuf holds a partially consumed chunk.
	dataCh  chan []byte
	readBuf []byte

	writeFn func([]byte) error // sends a DATA frame via the mux
	closeFn func()             // notifies the mux to send CLOSE_STREAM

	closeOnce sync.Once
	closed    chan struct{} // closed when stream is done

	// wrMu serialises Write calls so a single DATA frame is not interleaved.
	wrMu sync.Mutex
}

func newStream(id uint32, writeFn func([]byte) error, closeFn func()) *Stream {
	return &Stream{
		ID:      id,
		dataCh:  make(chan []byte, 256),
		writeFn: writeFn,
		closeFn: closeFn,
		closed:  make(chan struct{}),
	}
}

// Read reads incoming data from the stream.
// It blocks until data is available or the stream is closed.
func (s *Stream) Read(p []byte) (int, error) {
	for {
		// Drain leftover bytes from a previous chunk first.
		if len(s.readBuf) > 0 {
			n := copy(p, s.readBuf)
			s.readBuf = s.readBuf[n:]
			return n, nil
		}

		select {
		case data, ok := <-s.dataCh:
			if !ok {
				return 0, io.EOF
			}
			n := copy(p, data)
			if n < len(data) {
				s.readBuf = data[n:]
			}
			return n, nil
		case <-s.closed:
			// Drain any remaining data in the channel before returning EOF.
			select {
			case data, ok := <-s.dataCh:
				if !ok {
					return 0, io.EOF
				}
				n := copy(p, data)
				if n < len(data) {
					s.readBuf = data[n:]
				}
				return n, nil
			default:
				return 0, io.EOF
			}
		}
	}
}

// Write sends data over the stream as a DATA frame.
func (s *Stream) Write(p []byte) (int, error) {
	select {
	case <-s.closed:
		return 0, ErrStreamClosed
	default:
	}

	s.wrMu.Lock()
	defer s.wrMu.Unlock()

	// Re-check after acquiring lock.
	select {
	case <-s.closed:
		return 0, ErrStreamClosed
	default:
	}

	// Copy so caller can reuse p.
	buf := make([]byte, len(p))
	copy(buf, p)
	if err := s.writeFn(buf); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close closes the stream. It is safe to call multiple times.
func (s *Stream) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
		if s.closeFn != nil {
			s.closeFn()
		}
	})
	return nil
}

// isClosed reports whether the stream has been closed.
func (s *Stream) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// pushData delivers incoming data to the stream's read side.
// Called by the mux readLoop.
func (s *Stream) pushData(data []byte) {
	select {
	case s.dataCh <- data:
	case <-s.closed:
	}
}

// closeRead shuts down the read side of the stream (remote sent CLOSE_STREAM).
func (s *Stream) closeRead() {
	s.closeOnce.Do(func() {
		close(s.closed)
	})
}
