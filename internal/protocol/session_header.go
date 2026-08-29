// Package protocol defines RemLink wire-level constants and codecs.
package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// SessionHeaderSize is the fixed v1 Session header length in bytes.
	SessionHeaderSize = 20
	// SessionVersion is the only Session protocol version supported by v1.
	SessionVersion uint8 = 1
	// SessionTypeIPv4 marks a payload as one complete raw IPv4 packet.
	SessionTypeIPv4 uint8 = 0x01
)

var (
	sessionMagic = [4]byte{'R', 'M', 'L', 'K'}

	ErrSessionHeaderTooShort = errors.New("session packet is shorter than the 20-byte header")
	ErrSessionMagic          = errors.New("invalid session magic")
	ErrSessionVersion        = errors.New("unsupported session version")
	ErrSessionType           = errors.New("unsupported session payload type")
	ErrSessionFlags          = errors.New("session flags must be zero in v1")
	ErrSessionReserved       = errors.New("session reserved field must be zero in v1")
	ErrSessionPayloadLength  = errors.New("session payload length mismatch")
	ErrSessionPayloadTooLong = errors.New("session payload exceeds uint16 length")
)

// SessionHeader is the fixed 20-byte prefix of every Remote Subnet datagram.
// Multi-byte fields are encoded in network byte order (big-endian).
type SessionHeader struct {
	Version    uint8
	Type       uint8
	Flags      uint16
	SessionID  uint64
	PayloadLen uint16
	Reserved   uint16
}

// NewIPv4SessionHeader constructs the canonical v1 header for a raw IPv4 payload.
func NewIPv4SessionHeader(sessionID uint64, payloadLen uint16) SessionHeader {
	return SessionHeader{
		Version:    SessionVersion,
		Type:       SessionTypeIPv4,
		SessionID:  sessionID,
		PayloadLen: payloadLen,
	}
}

// Validate checks all v1 constants and reserved fields.
func (h SessionHeader) Validate() error {
	switch {
	case h.Version != SessionVersion:
		return fmt.Errorf("%w: got %d", ErrSessionVersion, h.Version)
	case h.Type != SessionTypeIPv4:
		return fmt.Errorf("%w: got 0x%02x", ErrSessionType, h.Type)
	case h.Flags != 0:
		return fmt.Errorf("%w: got 0x%04x", ErrSessionFlags, h.Flags)
	case h.Reserved != 0:
		return fmt.Errorf("%w: got 0x%04x", ErrSessionReserved, h.Reserved)
	default:
		return nil
	}
}

// MarshalBinary serializes only the fixed header.
func (h SessionHeader) MarshalBinary() ([]byte, error) {
	if err := h.Validate(); err != nil {
		return nil, err
	}

	out := make([]byte, SessionHeaderSize)
	copy(out[0:4], sessionMagic[:])
	out[4] = h.Version
	out[5] = h.Type
	binary.BigEndian.PutUint16(out[6:8], h.Flags)
	binary.BigEndian.PutUint64(out[8:16], h.SessionID)
	binary.BigEndian.PutUint16(out[16:18], h.PayloadLen)
	binary.BigEndian.PutUint16(out[18:20], h.Reserved)
	return out, nil
}

// ParseSessionHeader parses and validates a fixed header from the start of data.
func ParseSessionHeader(data []byte) (SessionHeader, error) {
	if len(data) < SessionHeaderSize {
		return SessionHeader{}, ErrSessionHeaderTooShort
	}
	if !bytes.Equal(data[0:4], sessionMagic[:]) {
		return SessionHeader{}, ErrSessionMagic
	}

	header := SessionHeader{
		Version:    data[4],
		Type:       data[5],
		Flags:      binary.BigEndian.Uint16(data[6:8]),
		SessionID:  binary.BigEndian.Uint64(data[8:16]),
		PayloadLen: binary.BigEndian.Uint16(data[16:18]),
		Reserved:   binary.BigEndian.Uint16(data[18:20]),
	}
	if err := header.Validate(); err != nil {
		return SessionHeader{}, err
	}
	return header, nil
}

// EncodeIPv4Session serializes a header and one complete raw IPv4 packet.
func EncodeIPv4Session(sessionID uint64, packet []byte) ([]byte, error) {
	if len(packet) > int(^uint16(0)) {
		return nil, ErrSessionPayloadTooLong
	}
	header := NewIPv4SessionHeader(sessionID, uint16(len(packet)))
	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return nil, err
	}

	out := make([]byte, SessionHeaderSize+len(packet))
	copy(out, headerBytes)
	copy(out[SessionHeaderSize:], packet)
	return out, nil
}

// DecodeIPv4Session validates framing and returns the header and raw IPv4 payload.
// The returned payload aliases data and must be copied before data is reused.
func DecodeIPv4Session(data []byte) (SessionHeader, []byte, error) {
	header, err := ParseSessionHeader(data)
	if err != nil {
		return SessionHeader{}, nil, err
	}
	payload := data[SessionHeaderSize:]
	if len(payload) != int(header.PayloadLen) {
		return SessionHeader{}, nil, fmt.Errorf(
			"%w: header=%d actual=%d",
			ErrSessionPayloadLength,
			header.PayloadLen,
			len(payload),
		)
	}
	return header, payload, nil
}
