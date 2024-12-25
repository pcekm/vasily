// A UDP backend that receives ICMP errors on a separate icmpbase.Conn.
//
//go:build rawsock || !linux

package udp

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"syscall"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/backend/icmpbase"
	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// Conn is a UDP ping connection.
type Conn struct {
	ipVer    util.IPVersion
	icmpConn *icmpbase.Conn

	mu       sync.Mutex
	connV4   *ipv4.PacketConn
	connV6   *ipv6.PacketConn
	basePort int
}

// New opens a new connection.
func New(ipVer util.IPVersion) (*Conn, error) {
	c := &Conn{
		ipVer:    ipVer,
		basePort: defaultBasePort,
	}

	address := util.Choose(ipVer, "udp4", "udp6")
	conn, err := net.ListenUDP(address, nil)
	if err != nil {
		return nil, err
	}
	switch ipVer {
	case util.IPv4:
		c.connV4 = ipv4.NewPacketConn(conn)
	case util.IPv6:
		c.connV6 = ipv6.NewPacketConn(conn)
	default:
		log.Panicf("Unknown IP version: %v", ipVer)
	}

	c.icmpConn, err = icmpbase.New(ipVer, util.Port(conn.LocalAddr()), syscall.IPPROTO_UDP)
	if err != nil {
		conn.Close()
		return nil, err
	}

	return c, nil
}

// SeqBasePort returns the base port number that sequence numbers are added to.
func (c *Conn) SeqBasePort() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.basePort
}

// SetSeqBasePort sets the base port number to add to sequence numbers.
func (c *Conn) SetSeqBasePort(p int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.basePort = p
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
	addr.Port = c.basePort + pkt.Seq

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
		return util.Port(c.connV4.LocalAddr())
	case util.IPv6:
		return util.Port(c.connV6.LocalAddr())
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
	pkt.Seq -= c.SeqBasePort()
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
