// Package subnet transports complete raw IPv4 packets inside Overlay UDP.
package subnet

import "remlink/internal/protocol"

// EncodeDatagram uses the one authoritative v1 SessionHeader codec.
func EncodeDatagram(sessionID uint64, packet []byte) ([]byte, error) {
	return protocol.EncodeIPv4Session(sessionID, packet)
}
