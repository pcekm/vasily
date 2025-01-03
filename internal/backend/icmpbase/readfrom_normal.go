// This is the ReadFrom that works pretty much everywhere but Linux.
//
//go:build rawsock || !linux

package icmpbase

import (
	"errors"
	"fmt"
	"net"

	"github.com/pcekm/vasily/internal/backend"
	"github.com/pcekm/vasily/internal/util/icmppkt"
)

// ReadFrom Reads an ICMP message.
func (c *internalConn) ReadFrom() (*backend.Packet, net.Addr, listenerKey, error) {
	buf := make([]byte, maxMTU)
	n, peer, err := c.conn.ReadFrom(buf)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) {
			if op.Timeout() {
				return nil, nil, listenerKey{}, backend.ErrTimeout
			}
		}
		return nil, peer, listenerKey{}, fmt.Errorf("read error: %v", err)
	}

	pkt, id, proto, err := icmppkt.Parse(c.ipVer, buf[:n])
	return pkt, peer, listenerKey{ID: id, Proto: proto}, err
}
