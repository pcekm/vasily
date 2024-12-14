// Package udp implements UDP pings.
package udp

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/backend/icmpbase"
	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	maxMTU = 1500

	udpProtoNum = 17

	icmpV4CodePortUnreachable = 3
	icmpV6CodePortUnreachable = 4

	ipv6FragmentType   = 44
	ipv6FragmentExtLen = 8

	// https://www.iana.org/assignments/service-names-port-numbers/service-names-port-numbers.xhtml?search=33434
	basePort = 33434
)

func init() {
	backend.Register("udp", func(ipVer util.IPVersion) (backend.Conn, error) { return New(ipVer) })
}

// Conn is a UDP ping connection.
type Conn struct {
	ipVer    util.IPVersion
	icmpConn *icmpbase.Conn

	mu     sync.Mutex
	connV4 *ipv4.PacketConn
	connV6 *ipv6.PacketConn
}

// New opens a new connection.
func New(ipVer util.IPVersion) (*Conn, error) {
	icmpConn, err := icmpbase.New(ipVer)
	if err != nil {
		return nil, err
	}
	c := &Conn{
		ipVer:    ipVer,
		icmpConn: icmpConn,
	}
	switch ipVer {
	case util.IPv4:
		conn, err := net.ListenUDP("udp4", nil)
		if err != nil {
			icmpConn.Close()
			return nil, err
		}
		c.connV4 = ipv4.NewPacketConn(conn)
	case util.IPv6:
		conn, err := net.ListenUDP("udp6", nil)
		if err != nil {
			icmpConn.Close()
			return nil, err
		}
		c.connV6 = ipv6.NewPacketConn(conn)
	default:
		log.Panicf("Unknown IP version: %v", ipVer)
	}

	icmpConn.SetExpectedSrcPort(c.localPort())

	return c, nil
}

// WriteTo sends a request.
func (c *Conn) WriteTo(pkt *backend.Packet, dest net.Addr, opts ...backend.WriteOption) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, o := range opts {
		if o, ok := o.(backend.TTLOption); ok {
			orig, err := c.ttl()
			if err != nil {
				return fmt.Errorf("can't get original TTL: %v", err)
			}
			defer func() {
				if err := c.setTTL(orig); err != nil {
					log.Printf("Error setting original TTL: %v", err)
				}
			}()
			c.setTTL(o.TTL)
		}
	}

	addr := *(dest.(*net.UDPAddr))
	addr.Port = basePort + pkt.Seq

	switch c.ipVer {
	case util.IPv4:
		_, err := c.connV4.WriteTo(pkt.Payload, nil, &addr)
		return err
	case util.IPv6:
		_, err := c.connV6.WriteTo(pkt.Payload, nil, &addr)
		return err
	}
	log.Panic("Unreachable case.")
	return nil
}

func (c *Conn) ttl() (int, error) {
	switch c.ipVer {
	case util.IPv4:
		return c.connV4.TTL()
	case util.IPv6:
		return c.connV6.HopLimit()
	default:
		return -1, nil
	}
}

func (c *Conn) setTTL(ttl int) error {
	switch c.ipVer {
	case util.IPv4:
		return c.connV4.SetTTL(ttl)
	case util.IPv6:
		return c.connV6.SetHopLimit(ttl)
	default:
		return nil
	}
}

func (c *Conn) localPort() int {
	switch c.ipVer {
	case util.IPv4:
		return c.connV4.LocalAddr().(*net.UDPAddr).Port
	case util.IPv6:
		return c.connV6.LocalAddr().(*net.UDPAddr).Port
	default:
		return -1
	}
}

// ReadFrom receives a reply. The received packet will likely not include any
// payload that was initially sent.
func (c *Conn) ReadFrom(ctx context.Context) (*backend.Packet, net.Addr, error) {
	pkt, peer, err := c.icmpConn.ReadFrom(ctx)
	if err != nil {
		return nil, nil, err
	}
	pkt.Seq -= basePort
	return pkt, peer, err
}

// Close closes the connection.
func (c *Conn) Close() error {
	switch c.ipVer {
	case util.IPv4:
		return c.connV4.Close()
	case util.IPv6:
		return c.connV6.Close()
	default:
		return nil
	}
}
