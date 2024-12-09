// Package icmp is an implementation of an ICMP pinger.
package icmp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.org/x/time/rate"
)

const (
	icmpV4ProtoNum  = 1
	icmpV6ProtoNum  = 58
	maxMTU          = 1500
	minPingInterval = time.Second
	maxActiveConns  = 100
)

// Sent to when a connection is created; received from when a connection is
// closed. This limits the total number of connections since the initial send
// will block (or fail) if the buffer is full.
var activeConns = make(chan any, maxActiveConns)

// PingConn is a basic ping network connection. A connection may handle either
// IPv4 or IPv6 but not both at the same time. Since this may run setuid root,
// the total number of open connections is limited.
type PingConn struct {
	protoNum int
	icmpType icmp.Type
	pingID   int
	limiter  *rate.Limiter

	// Write operations are locked so that TTL can be set and reset atomically.
	// Uses write locks for custom TTLs, and read locks for sends on the default
	// TTL. This allows concurrent writes for the more common case, and only
	// fully locks to set the TTL, write, and reset the TTL atomically.
	ttlMu  sync.RWMutex
	readMu sync.Mutex
	conn   *icmp.PacketConn
}

// New creates a new ICMP ping connection. The network arg should be:
func New(ipVer util.IPVersion) (*PingConn, error) {
	select {
	case activeConns <- nil:
	default:
		return nil, errors.New("too many connections")
	}

	protoNum := icmpV4ProtoNum
	icmpType := icmp.Type(ipv4.ICMPTypeEcho)
	if ipVer == util.IPv6 {
		protoNum = icmpV6ProtoNum
		icmpType = ipv6.ICMPTypeEchoRequest
	}

	conn, err := newConn(ipVer)
	if err != nil {
		return nil, fmt.Errorf("listen error: %v", err)
	}
	pingID, err := pingID(conn)
	if err != nil {
		return nil, err
	}
	p := &PingConn{
		protoNum: protoNum,
		icmpType: icmpType,
		pingID:   pingID,
		limiter:  rate.NewLimiter(rate.Every(minPingInterval), 5),
		conn:     conn,
	}

	return p, nil
}

// Close closes the connection.
func (p *PingConn) Close() error {
	err := p.conn.Close()
	<-activeConns
	return err
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
	// if err := p.limiter.Wait(context.TODO()); err != nil {
	// 	return fmt.Errorf("rate limiter: %v", err)
	// }
	if !p.limiter.Allow() {
		return errors.New("rate limit exceeded")
	}
	dest = wrangleAddr(dest)
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
	p.ttlMu.RLock()
	defer p.ttlMu.RUnlock()
	return p.baseWriteTo(pkt, dest)
}

// writeToTTL sends an ICMP echo request with a given time to live.
func (p *PingConn) writeToTTL(pkt *backend.Packet, dest net.Addr, ttl int) error {
	p.ttlMu.Lock()
	defer p.ttlMu.Unlock()
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
			ID:   p.pingID,
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

// Reads an ICMP echo response.
func (p *PingConn) ReadFrom(ctx context.Context) (*backend.Packet, net.Addr, error) {
	buf := make([]byte, maxMTU)
	for {
		if dl, ok := ctx.Deadline(); ok {
			if err := p.conn.SetReadDeadline(dl); err != nil {
				return nil, nil, err
			}
		} else if err := p.conn.SetReadDeadline(time.Time{}); err != nil {
			return nil, nil, err
		}
		n, peer, err := p.conn.ReadFrom(buf)
		if err != nil {
			if strings.HasSuffix(err.Error(), "timeout") {
				return nil, peer, backend.ErrTimeout
			}
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
			continue
		}
		if rm.Type != ipv4.ICMPTypeEchoReply && rm.Type != ipv6.ICMPTypeEchoReply {
			pkt, id, err := icmpMessageToPacket(rm)
			// Filter out unrelated IDs.
			if err == nil && id != p.pingID {
				continue
			}
			return pkt, peer, err
		}
		pkt, id := echoToPacket(rm.Body.(*icmp.Echo))
		if id != p.pingID {
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

func icmpMessageToPacket(msg *icmp.Message) (*backend.Packet, int, error) {
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

	retICMP, err := icmp.ParseMessage(icmpV4ProtoNum, bodyData[ipHeader.Len:])
	if err != nil {
		return nil, 0, fmt.Errorf("error parsing TimeExceeded body: %v", err)
	}

	if retICMP.Type != ipv4.ICMPTypeEcho {
		return nil, 0, fmt.Errorf("unexpected ICMP type: %v", retICMP.Type)
	}
	pkt, id := echoToPacket(retICMP.Body.(*icmp.Echo))
	pkt.Type = packetType
	return pkt, id, nil
}
