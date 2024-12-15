// This is the ReadFrom that works pretty much everywhere but Linux.
//
//go:build rawsock || !linux

package icmpbase

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/icmppkt"
)

// ReadFrom Reads an ICMP message.
func (c *Conn) ReadFrom(ctx context.Context) (*backend.Packet, net.Addr, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	if dl, ok := ctx.Deadline(); ok {
		if err := c.conn.SetReadDeadline(dl); err != nil {
			return nil, nil, err
		}
	} else if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
		return nil, nil, err
	}

	for {
		buf := make([]byte, maxMTU)
		n, peer, err := c.conn.ReadFrom(buf)
		if err != nil {
			var op *net.OpError
			if errors.As(err, &op) {
				if op.Timeout() {
					return nil, nil, backend.ErrTimeout
				}
			}
			return nil, peer, fmt.Errorf("read error: %v", err)
		}

		pkt, id, err := icmppkt.Parse(c.ipVer, buf[:n])
		if id != c.EchoID() || pkt.Type == backend.PacketRequest {
			continue
		}
		return pkt, peer, err
	}
}
