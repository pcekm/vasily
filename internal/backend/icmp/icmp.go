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

func baseNew(ipVer util.IPVersion, mkConn func(util.IPVersion, int, int) (*icmpbase.Conn, error)) (*PingConn, error) {
	conn, err := mkConn(ipVer, 0, ipVer.ICMPProtoNum())
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
	return p.conn.ReadFrom(ctx)
}
