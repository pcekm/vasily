// Package backend contains a lower-level interface for ping connections.
//
// Backends may be ICMP, or the could be UDP or something else.
package backend

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"

	"github.com/pcekm/vasily/internal/util"
	"github.com/spf13/pflag"
)

var (
	registry      = make(map[Name]NewConnFunc)
	privsepClient PrivsepClient

	// ErrTimeout indicates that an operation reached its timeout or deadline.
	// TODO: This should probably be replaced with net.Error.Timeout().
	ErrTimeout = errors.New("timeout")
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

// PortConn is an extended interface for connections that map sequence numbers
// onto port numbers.
type PortConn interface {
	Conn

	// SeqBasePort returns the base port number to which sequence numbers are
	// added. Implementations should have a reasonable default for this number.
	SeqBasePort() int

	// SetSeqBasePort sets the port number for sequence 0. Port numbers used by
	// the backend will be pkt.Seq + this base port number.
	SetSeqBasePort(p int)
}

// Name is the name of a backend.
type Name string

// New creates a new connection.
func New(name Name, ipVer util.IPVersion) (Conn, error) {
	if privsepClient != nil {
		return privsepClient.NewConn(name, ipVer)
	}
	nc, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("invalid backend %q", name)
	}
	return nc(ipVer)
}

// NewConnFunc is a function that creates a connection.
type NewConnFunc func(util.IPVersion) (Conn, error)

// Register configures a new backend.
func Register(n Name, nc NewConnFunc) {
	registry[n] = nc
}

// PrivsepClient is the required interface for the privsep client.
type PrivsepClient interface {
	NewConn(Name, util.IPVersion) (Conn, error)
}

// UsePrivsep configures [New] to return connections that work via the privsep
// server.
func UsePrivsep(client PrivsepClient) {
	privsepClient = client
}

type flagValue string

func (f flagValue) String() string {
	return string(f)
}

func (f *flagValue) Set(name string) error {
	if _, ok := registry[Name(name)]; !ok {
		return fmt.Errorf("invalid backend %q", name)
	}
	*f = flagValue(name)
	return nil
}

func (f *flagValue) Type() string {
	var names []string
	for n := range registry {
		names = append(names, string(n))
	}
	slices.Sort(names)
	return strings.Join(names, "|")
}

// FlagP returns a flag for selecting a backend.
func FlagP(name, shorthand, value, usage string) *Name {
	pflag.VarP((*flagValue)(&value), name, shorthand, usage)
	return (*Name)(&value)
}
