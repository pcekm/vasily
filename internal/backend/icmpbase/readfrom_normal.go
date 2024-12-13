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
)

// ReadFrom Reads an ICMP message.
func (c *Conn) ReadFrom(ctx context.Context, buf []byte) (int, net.Addr, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	if dl, ok := ctx.Deadline(); ok {
		if err := c.conn.SetReadDeadline(dl); err != nil {
			return -1, nil, err
		}
	} else if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
		return -1, nil, err
	}
	n, peer, err := c.conn.ReadFrom(buf)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) {
			if op.Timeout() {
				return -1, nil, backend.ErrTimeout
			}
		}
		return -1, peer, fmt.Errorf("connection read error: %v", err)
	}

	return n, peer, nil
}
