//go:build !windows

// Package icmp is an implementation of an ICMP pinger.
package icmp

import (
	"context"
	"fmt"
	"net"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/backend/icmpbase"
	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	maxMTU = 1500
)

func init() {
	backend.Register("icmp", func(v util.IPVersion) (backend.Conn, error) { return New(v) })
}

// PingConn is a basic ping network connection. A connection may handle either
// IPv4 or IPv6 but not both at the same time. Since this may run setuid root,
// the total number of open connections is limited.
type PingConn struct {
	ipVer    util.IPVersion
	icmpType icmp.Type

	conn *icmpbase.Conn
}

// New creates a new ICMP ping connection. The network arg should be:
func New(ipVer util.IPVersion) (*PingConn, error) {
	return baseNew(ipVer, icmpbase.New)
}

func baseNew(ipVer util.IPVersion, mkConn func(util.IPVersion) (*icmpbase.Conn, error)) (*PingConn, error) {
	conn, err := mkConn(ipVer)
	if err != nil {
		return nil, err
	}

	icmpType := icmp.Type(ipv4.ICMPTypeEcho)
	if ipVer == util.IPv6 {
		icmpType = ipv6.ICMPTypeEchoRequest
	}

	return &PingConn{
		ipVer:    ipVer,
		icmpType: icmpType,
		conn:     conn,
	}, nil
}

// Close closes the connection.
func (p *PingConn) Close() error {
	return p.conn.Close()
}

// WriteTo sends an ICMP echo request.
func (p *PingConn) WriteTo(pkt *backend.Packet, dest net.Addr, opts ...backend.WriteOption) error {
	if pkt.Type != backend.PacketRequest {
		return fmt.Errorf("packet type must be %v (got %v)", backend.PacketReply, pkt.Type)
	}
	wm := icmp.Message{
		Type: p.icmpType,
		Code: 0,
		Body: &icmp.Echo{
			ID:   p.conn.EchoID(),
			Seq:  pkt.Seq,
			Data: pkt.Payload,
		},
	}
	buf, err := wm.Marshal(nil)
	if err != nil {
		return fmt.Errorf("marshal: %v", err)
	}
	return p.conn.WriteTo(buf, dest, opts...)
}

// Reads an ICMP echo response.
func (p *PingConn) ReadFrom(ctx context.Context) (*backend.Packet, net.Addr, error) {
	buf := make([]byte, maxMTU)
	for {
		n, peer, err := p.conn.ReadFrom(ctx, buf)
		if err != nil {
			return nil, peer, err
		}
		rm, err := icmp.ParseMessage(p.ipVer.ICMPProtoNum(), buf[:n])
		if err != nil {
			return nil, nil, fmt.Errorf("parsing message: %v", err)
		}

		if rm.Type == ipv6.ICMPTypeEchoRequest {
			// Weirdly, on MacOS, this sometimes receives the _sent_ ipv6 packet.
			// Ignore it and wait for another packet.
			//
			// TODO: Is this a bug or expected behavior?
			//  - Does it happen pinging something other than the loopback?
			//  - Does it happen on Linux?
			//  - Does it happen with privileged pings?
			continue
		}
		if rm.Type != ipv4.ICMPTypeEchoReply && rm.Type != ipv6.ICMPTypeEchoReply {
			pkt, id, err := p.icmpMessageToPacket(rm)
			// Filter out unrelated IDs.
			if err == nil && id != p.conn.EchoID() {
				continue
			}
			return pkt, peer, err
		}
		pkt, id := echoToPacket(rm.Body.(*icmp.Echo))
		if id != p.conn.EchoID() {
			continue
		}
		return pkt, peer, nil
	}
}

func echoToPacket(msg *icmp.Echo) (*backend.Packet, int) {
	return &backend.Packet{
		Type:    backend.PacketReply,
		Seq:     msg.Seq,
		Payload: msg.Data,
	}, msg.ID
}

func (p *PingConn) icmpMessageToPacket(msg *icmp.Message) (*backend.Packet, int, error) {
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
		return nil, 0, fmt.Errorf("unhandled ICMP message: %#v", msg)
	}

	ipHeader, err := ipv4.ParseHeader(bodyData)
	if err != nil {
		return nil, 0, fmt.Errorf("error parsing TimeExceeded header: %v", err)
	}

	retICMP, err := icmp.ParseMessage(p.ipVer.ICMPProtoNum(), bodyData[ipHeader.Len:])
	if err != nil {
		return nil, 0, fmt.Errorf("error parsing TimeExceeded body: %v", err)
	}

	if retICMP.Type != ipv4.ICMPTypeEcho && retICMP.Type != ipv6.ICMPTypeEchoRequest {
		// Unrelated message.
		return nil, -1, nil
	}
	pkt, id := echoToPacket(retICMP.Body.(*icmp.Echo))
	pkt.Type = packetType
	return pkt, id, nil
}
