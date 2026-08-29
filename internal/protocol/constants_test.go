package protocol

import (
	"testing"
	"time"
)

func TestControlMessageTypesMatchV1Spec(t *testing.T) {
	t.Parallel()
	if got, want := len(ControlMessageTypes), 13; got != want {
		t.Fatalf("ControlMessageTypes length = %d, want %d", got, want)
	}
	seen := make(map[ControlMessageType]struct{}, len(ControlMessageTypes))
	for _, messageType := range ControlMessageTypes {
		if !messageType.Valid() {
			t.Fatalf("listed Control message type %q is invalid", messageType)
		}
		if _, duplicate := seen[messageType]; duplicate {
			t.Fatalf("duplicate Control message type %q", messageType)
		}
		seen[messageType] = struct{}{}
	}
	if ControlMessageType("SESSION_RESUME").Valid() {
		t.Fatal("non-v1 SESSION_RESUME unexpectedly accepted")
	}
}

func TestControlEnvelopeRoundTripAndStrictPayload(t *testing.T) {
	envelope, err := NewControlEnvelope(ControlHeartbeat, "request-1", HeartbeatPayload{
		Timestamp: time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC), Status: "OK",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload HeartbeatPayload
	if err := envelope.DecodePayload(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "OK" || envelope.RequestID != "request-1" {
		t.Fatalf("unexpected envelope: %+v payload=%+v", envelope, payload)
	}
	envelope.Payload = []byte(`{"timestamp":"2026-08-25T00:00:00Z","status":"OK","unknown":true}`)
	if err := envelope.DecodePayload(&payload); err == nil {
		t.Fatal("unknown Control payload field was accepted")
	}
}

func TestErrorCodesMatchAuthoritativeV1List(t *testing.T) {
	t.Parallel()
	if got, want := len(ErrorCodes), 14; got != want {
		t.Fatalf("ErrorCodes length = %d, want %d", got, want)
	}
	seen := make(map[ErrorCode]struct{}, len(ErrorCodes))
	for _, code := range ErrorCodes {
		if !code.Valid() {
			t.Fatalf("listed error code %q is invalid", code)
		}
		if _, duplicate := seen[code]; duplicate {
			t.Fatalf("duplicate error code %q", code)
		}
		seen[code] = struct{}{}
	}
	if ErrorCode("UNKNOWN").Valid() {
		t.Fatal("unknown error code unexpectedly accepted")
	}
}
