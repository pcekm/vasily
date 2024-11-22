// Package icmp is an implementation of an ICMP pinger.
package icmp

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pcekm/graphping/internal/backend"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	icmpV4ProtoNum = 1
	icmpV6ProtoNum = 58
	maxMTU         = 1500
)

// PingConn is a basic ping network connection. A connection may handle either
// IPv4 or IPv6 but not both at the same time.
type PingConn struct {
	protoNum int
	icmpType icmp.Type

	mu sync.RWMutex
	// Write operations are locked so that TTL can be set and reset atomically.
	// Uses write locks for custom TTLs, and read locks for sends on the default
	// TTL. This allows concurrent writes for the more common case, and only
	// fully locks to set the TTL, write, and reset the TTL atomically.
	conn *icmp.PacketConn
}

// New creates a new ICMP ping connection. The network arg should be:
//
//	"udp4":          Unprivileged IPv4 ping on MacOS or Linux
//	"udp6":          Unprivileged IPV6 ping on MacOS or Linux
//	"ip4:icmp":      Privileged, raw-socket IPv4 ping
//	"ip6:ipv6-icmp": Privileged, raw socket IPv6 ping
//
// Addr is the interface address to lissen on. An empty string means all
// interfaces. See icmp.PacketConn.ListenPacket() for more information.
func New(network string, addr string) (*PingConn, error) {
	protoNum := icmpV4ProtoNum
	icmpType := icmp.Type(ipv4.ICMPTypeEcho)
	if strings.HasSuffix(network, "6") {
		protoNum = icmpV6ProtoNum
		icmpType = ipv6.ICMPTypeEchoRequest
	}

	conn, err := icmp.ListenPacket(network, addr)
	if err != nil {
		return nil, fmt.Errorf("listen error: %v", err)
	}
	p := &PingConn{
		protoNum: protoNum,
		icmpType: icmpType,
		conn:     conn,
	}
	return p, nil
}

// Close closes the connection.
func (p *PingConn) Close() error {
	return p.conn.Close()
}

// SetDeadline sets the read and write deadlines.
func (p *PingConn) SetDeadline(t time.Time) error {
	return p.conn.SetDeadline(t)
}

// SetReadDeadline sets the read deadline.
func (p *PingConn) SetReadDeadline(t time.Time) error {
	return p.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline.
func (p *PingConn) SetWriteDeadline(t time.Time) error {
	return p.conn.SetWriteDeadline(t)
}

// Sets the time to live of sent packets.
func (p *PingConn) setTTL(ttl int) error {
	switch p.protoNum {
	case icmpV4ProtoNum:
		return p.conn.IPv4PacketConn().SetTTL(ttl)
	case icmpV6ProtoNum:
		return p.conn.IPv6PacketConn().SetHopLimit(ttl)
	default:
		log.Panicf("Invalid protonum: %d", p.protoNum)
	}
	return nil
}

// Gets the time to live of sent packets.
func (p *PingConn) ttl() (int, error) {
	switch p.protoNum {
	case icmpV4ProtoNum:
		return p.conn.IPv4PacketConn().TTL()
	case icmpV6ProtoNum:
		return p.conn.IPv6PacketConn().HopLimit()
	default:
		log.Panicf("Invalid protonum: %d", p.protoNum)
	}
	return 0, nil
}

// WriteTo sends an ICMP echo request.
func (p *PingConn) WriteTo(pkt *backend.Packet, dest net.Addr, opts ...backend.WriteOption) error {
	var withTTL int
	for _, o := range opts {
		switch o := o.(type) {
		case backend.TTLOption:
			withTTL = o.TTL
		default:
			log.Panicf("Unsupported option: %#v", o)
		}
	}
	if withTTL != 0 {
		return p.writeToTTL(pkt, dest, withTTL)
	}
	return p.writeToNormal(pkt, dest)
}

func (p *PingConn) writeToNormal(pkt *backend.Packet, dest net.Addr) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.baseWriteTo(pkt, dest)
}

// writeToTTL sends an ICMP echo request with a given time to live.
func (p *PingConn) writeToTTL(pkt *backend.Packet, dest net.Addr, ttl int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	origTTL, err := p.ttl()
	if err != nil {
		return fmt.Errorf("unable to get current ttl: %v", err)
	}
	defer func() {
		if err := p.setTTL(origTTL); err != nil {
			log.Printf("Unable to set ttl: %v", err)
		}
	}()
	if err := p.setTTL(ttl); err != nil {
		return fmt.Errorf("unable to set ttl: %v", err)
	}
	return p.baseWriteTo(pkt, dest)
}

// Core writeTo function. Callers must hold p.mu.
func (p *PingConn) baseWriteTo(pkt *backend.Packet, dest net.Addr) error {
	if pkt.Type != backend.PacketRequest {
		return fmt.Errorf("packet type must be %v (got %v)", backend.PacketReply, pkt.Type)
	}

	wm := icmp.Message{
		Type: p.icmpType,
		Code: 0,
		Body: &icmp.Echo{
			ID:   pkt.ID,
			Seq:  pkt.Seq,
			Data: pkt.Payload,
		},
	}
	wb, err := wm.Marshal(nil)
	if err != nil {
		return fmt.Errorf("marshal error: %v", err)
	}

	if _, err := p.conn.WriteTo(wb, dest); err != nil {
		return err
	}
	return nil
}

// Reads an ICMP echo reponse.
func (p *PingConn) ReadFrom() (*backend.Packet, net.Addr, error) {
	buf := make([]byte, maxMTU)
	for {
		n, peer, err := p.conn.ReadFrom(buf)
		if err != nil {
			return nil, peer, fmt.Errorf("connection read error: %v", err)
		}

		rm, err := icmp.ParseMessage(p.protoNum, buf[:n])
		if err != nil {
			return nil, peer, fmt.Errorf("error parsing ICMP message: %v", err)
		}
		if rm.Type == ipv6.ICMPTypeEchoRequest {
			// Weirdly, on MacOS, this sometimes receives the _sent_ ipv6 packet.
			// Ignore it and wait for another packet.
			//
			// TODO: Is this a bug or expected behavior?
			//  - Does it happen pinging something other than the loopback?
			//  - Does it happen on Linux?
			//  - Does it happen with privileged pings?
			log.Printf("Unexpectedly received ipv6 echo request: %#v", rm)
			continue
		}
		if rm.Type != ipv4.ICMPTypeEchoReply && rm.Type != ipv6.ICMPTypeEchoReply {
			pkt, err := icmpMessageToPacket(rm)
			return pkt, peer, err
		}
		pkt, err := echoToPacket(rm.Body.(*icmp.Echo)), nil
		return pkt, peer, err
	}
}

func echoToPacket(msg *icmp.Echo) *backend.Packet {
	return &backend.Packet{
		Type:    backend.PacketReply,
		ID:      msg.ID,
		Seq:     msg.Seq,
		Payload: msg.Data,
	}
}

func icmpMessageToPacket(msg *icmp.Message) (*backend.Packet, error) {
	var packetType backend.PacketType
	var bodyData []byte

	switch body := msg.Body.(type) {
	case *icmp.TimeExceeded:
		packetType = backend.PacketTimeExceeded
		bodyData = body.Data
	case *icmp.DstUnreach:
		packetType = backend.PacketDestinationUnreachable
		bodyData = body.Data
	default:
		return nil, fmt.Errorf("unhandled ICMP message: %#v", msg)
	}

	ipHeader, err := ipv4.ParseHeader(bodyData)
	if err != nil {
		return nil, fmt.Errorf("error parsing TimeExceeded header: %v", err)
	}

	retICMP, err := icmp.ParseMessage(icmpV4ProtoNum, bodyData[ipHeader.Len:])
	if err != nil {
		return nil, fmt.Errorf("error parsing TimeExceeded body: %v", err)
	}

	if retICMP.Type != ipv4.ICMPTypeEcho {
		return nil, fmt.Errorf("unexpected ICMP type: %v", retICMP.Type)
	}
	pkt := echoToPacket(retICMP.Body.(*icmp.Echo))
	pkt.Type = packetType
	return pkt, nil
}
