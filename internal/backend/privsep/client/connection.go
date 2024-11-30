package client

import (
	"errors"
	"log"
	"net"
	"time"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/backend/privsep/messages"
)

// Connection is a single ping connection.
type Connection struct {
	client   *Client
	id       messages.ConnectionID
	readFrom chan messages.PingReply
	closed   chan error
}

// ID returns the connection ID. This is mostly for testing purposes.
func (c *Connection) ID() messages.ConnectionID {
	return c.id
}

// WriteTo writes a ping message to a remote host.
func (c *Connection) WriteTo(pkt *backend.Packet, dest net.Addr, opts ...backend.WriteOption) error {
	msg := messages.SendPing{
		ID:     c.id,
		Packet: *pkt,
		Addr:   dest.(*net.UDPAddr).IP,
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
func (c *Connection) ReadFrom() (pkt *backend.Packet, peer net.Addr, err error) {
	msg := <-c.readFrom
	return &msg.Packet, &net.UDPAddr{IP: msg.Peer}, nil
}

// SetDeadline sets the read and write deadlines for this connection.
func (c *Connection) SetDeadline(time.Time) error {
	return errors.New("not implemented")
}

// SetReadDeadline sets the read deadline for this connection.
func (c *Connection) SetReadDeadline(time.Time) error {
	return errors.New("not implemented")
}

// SetWriteDeadline sets the write deadline for this connection.
func (c *Connection) SetWriteDeadline(time.Time) error {
	return errors.New("not implemented")
}

// Closes the connection.
func (c *Connection) Close() error {
	if err := c.client.sendMessage(messages.CloseConnection{ID: c.id}); err != nil {
		return err
	}
	return <-c.closed
}
