package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Frame types for the multiplexing protocol.
const (
	FrameOpenStream  byte = 0x01
	FrameData        byte = 0x02
	FrameCloseStream byte = 0x03
	FramePing        byte = 0x04
	FramePong        byte = 0x05
)

// MaxPayloadSize is the maximum allowed payload size (10 MB).
const MaxPayloadSize = 10 * 1024 * 1024

// frameHeaderSize is the total header length: 1 (type) + 4 (stream_id) + 4 (payload_len).
const frameHeaderSize = 9

var (
	ErrPayloadTooLarge = errors.New("protocol: payload exceeds maximum size")
	ErrInvalidFrame    = errors.New("protocol: invalid frame type")
)

// Frame represents a single multiplexing protocol frame.
// Wire format: [1B type][4B stream_id][4B payload_len][NB payload], big-endian.
type Frame struct {
	Type     byte
	StreamID uint32
	Payload  []byte
}

// EncodeFrame serialises a Frame into its wire representation.
func EncodeFrame(f Frame) []byte {
	pLen := len(f.Payload)
	buf := make([]byte, frameHeaderSize+pLen)
	buf[0] = f.Type
	binary.BigEndian.PutUint32(buf[1:5], f.StreamID)
	binary.BigEndian.PutUint32(buf[5:9], uint32(pLen))
	copy(buf[9:], f.Payload)
	return buf
}

// DecodeFrame reads exactly one frame from r.
func DecodeFrame(r io.Reader) (Frame, error) {
	var hdr [frameHeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Frame{}, fmt.Errorf("protocol: reading frame header: %w", err)
	}

	fType := hdr[0]
	if fType < FrameOpenStream || fType > FramePong {
		return Frame{}, fmt.Errorf("%w: 0x%02x", ErrInvalidFrame, fType)
	}

	streamID := binary.BigEndian.Uint32(hdr[1:5])
	payloadLen := binary.BigEndian.Uint32(hdr[5:9])

	if payloadLen > MaxPayloadSize {
		return Frame{}, fmt.Errorf("%w: %d bytes", ErrPayloadTooLarge, payloadLen)
	}

	payload := make([]byte, payloadLen)
	if payloadLen > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return Frame{}, fmt.Errorf("protocol: reading frame payload: %w", err)
		}
	}

	return Frame{
		Type:     fType,
		StreamID: streamID,
		Payload:  payload,
	}, nil
}
