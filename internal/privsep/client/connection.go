package client

import (
	"context"
	"log"
	"net"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/privsep/messages"
	"github.com/pcekm/graphping/internal/util"
)

// Connection is a single ping connection.
type Connection struct {
	client   *Client
	id       messages.ConnectionID
	backend  backend.Name
	readFrom chan messages.PingReply
	closed   chan error
}

// ID returns the connection ID. This is mostly for testing purposes.
func (c *Connection) ID() messages.ConnectionID {
	return c.id
}

// Backend returns the name of the backend. This is mostly for testing.
func (c *Connection) Backend() backend.Name {
	return c.backend
}

// WriteTo writes a ping message to a remote host.
func (c *Connection) WriteTo(pkt *backend.Packet, dest net.Addr, opts ...backend.WriteOption) error {
	msg := messages.SendPing{
		ID:     c.id,
		Packet: *pkt,
		Addr:   util.IP(dest),
	}
	for _, o := range opts {
		switch o := o.(type) {
		case backend.TTLOption:
			msg.TTL = o.TTL
		default:
			log.Panicf("Unhandled backend.WriteOption: %#v", o)
		}
	}
	return c.client.sendMessage(msg)
}

// ReadFrom reads the next available ping reply.
func (c *Connection) ReadFrom(ctx context.Context) (pkt *backend.Packet, peer net.Addr, err error) {
	select {
	case msg := <-c.readFrom:
		return &msg.Packet, &net.UDPAddr{IP: msg.Peer}, nil
	case <-ctx.Done():
		return nil, nil, backend.ErrTimeout
	}
}

// Closes the connection.
func (c *Connection) Close() error {
	if err := c.client.sendMessage(messages.CloseConnection{ID: c.id}); err != nil {
		return err
	}
	return <-c.closed
}
