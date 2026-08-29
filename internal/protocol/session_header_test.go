package protocol

import (
	"bytes"
	"errors"
	"testing"
)

func TestSessionHeaderGoldenBytes(t *testing.T) {
	t.Parallel()
	header := NewIPv4SessionHeader(0x0102030405060708, 0x0014)
	got, err := header.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	want := []byte{
		'R', 'M', 'L', 'K',
		0x01, 0x01,
		0x00, 0x00,
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x00, 0x14,
		0x00, 0x00,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("MarshalBinary() = %x, want %x", got, want)
	}
}

func TestIPv4SessionRoundTrip(t *testing.T) {
	t.Parallel()
	packet := []byte{0x45, 0x00, 0x00, 0x04}
	encoded, err := EncodeIPv4Session(42, packet)
	if err != nil {
		t.Fatalf("EncodeIPv4Session() error = %v", err)
	}
	header, decoded, err := DecodeIPv4Session(encoded)
	if err != nil {
		t.Fatalf("DecodeIPv4Session() error = %v", err)
	}
	if header.SessionID != 42 {
		t.Fatalf("SessionID = %d, want 42", header.SessionID)
	}
	if !bytes.Equal(decoded, packet) {
		t.Fatalf("decoded payload = %x, want %x", decoded, packet)
	}
}

func TestDecodeIPv4SessionRejectsInvalidFraming(t *testing.T) {
	t.Parallel()
	valid, err := EncodeIPv4Session(7, []byte{0x45})
	if err != nil {
		t.Fatalf("EncodeIPv4Session() error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func([]byte) []byte
		want   error
	}{
		{name: "short", mutate: func(data []byte) []byte { return data[:19] }, want: ErrSessionHeaderTooShort},
		{name: "magic", mutate: func(data []byte) []byte { data[0] = 'X'; return data }, want: ErrSessionMagic},
		{name: "version", mutate: func(data []byte) []byte { data[4] = 2; return data }, want: ErrSessionVersion},
		{name: "type", mutate: func(data []byte) []byte { data[5] = 2; return data }, want: ErrSessionType},
		{name: "flags", mutate: func(data []byte) []byte { data[7] = 1; return data }, want: ErrSessionFlags},
		{name: "reserved", mutate: func(data []byte) []byte { data[19] = 1; return data }, want: ErrSessionReserved},
		{name: "payload length", mutate: func(data []byte) []byte { data[17] = 2; return data }, want: ErrSessionPayloadLength},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := append([]byte(nil), valid...)
			_, _, gotErr := DecodeIPv4Session(test.mutate(input))
			if !errors.Is(gotErr, test.want) {
				t.Fatalf("DecodeIPv4Session() error = %v, want %v", gotErr, test.want)
			}
		})
	}
}

func TestEncodeIPv4SessionRejectsOversizedPayload(t *testing.T) {
	t.Parallel()
	_, err := EncodeIPv4Session(1, make([]byte, 1<<16))
	if !errors.Is(err, ErrSessionPayloadTooLong) {
		t.Fatalf("EncodeIPv4Session() error = %v, want %v", err, ErrSessionPayloadTooLong)
	}
}
