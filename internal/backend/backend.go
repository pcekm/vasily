// Package backend contains a lower-level interface for ping connections.
//
// Backends may be ICMP, or the could be UDP or something else.
package backend

import (
	"context"
	"fmt"
	"net"
)

// PacketType is a type of ICMP packet.
type PacketType int

// Values for PacketType.
const (
	// PacketRequest is an ICMP echo request.
	PacketRequest PacketType = iota

	// PacketReply is an ICMP echo reply.
	PacketReply

	// PacketTimeExceeded is an ICMP TTL or time exceeded message.
	PacketTimeExceeded

	// PacketDestinationUnreachable is an ICMP destination unreachable message.
	PacketDestinationUnreachable
)

func (t PacketType) String() string {
	switch t {
	case PacketRequest:
		return "PacketRequest"
	case PacketReply:
		return "PacketReply"
	case PacketTimeExceeded:
		return "PacketTimeExceeded"
	case PacketDestinationUnreachable:
		return "PacketDestinationUnreachable"
	default:
		return fmt.Sprintf("(unknown:%d)", t)
	}
}

// Packet is a higher-level representation of a ping request or reply.
type Packet struct {
	// Type is the type of packet sent or received.
	Type PacketType

	// Seq is a number identifying a particular request/response pair in a ping
	// session.
	Seq int

	// Payload contains additional raw data sent in a ping request, or
	// received in a reply.
	Payload []byte
}

// WriteOption is an option that may be passed to WriteTo.
type WriteOption any

type TTLOption struct {
	TTL int
}

// Conn is the interface implemented by ping backend connections.
type Conn interface {
	// WriteTo writes a ping message to a remote host.
	WriteTo(pkt *Packet, dest net.Addr, opts ...WriteOption) error

	// ReadFrom reads the next available ping reply.
	ReadFrom(ctx context.Context) (pkt *Packet, peer net.Addr, err error)

	// Close closes the connection. As is standard with network connections in
	// Go, any blocked read or write operations will be unblocked and return
	// errors.
	Close() error
}

// NewConn is a function that creates a connection.
type NewConn func() (Conn, error)
