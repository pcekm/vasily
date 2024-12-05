// Package client is a client to the privsep server.
package client

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"github.com/pcekm/graphping/internal/privsep/messages"
	"github.com/pcekm/graphping/internal/util"
)

// Client is the client for the privsep server.
type Client struct {
	in            io.ReadCloser
	inb           *bufio.Reader
	openConnReply chan messages.OpenConnectionReply

	mu          sync.Mutex
	out         io.WriteCloser
	connections map[messages.ConnectionID]*Connection
}

// New creates a new client.
func New(in io.ReadCloser, out io.WriteCloser) *Client {
	c := &Client{
		in:            in,
		inb:           bufio.NewReader(in),
		out:           out,
		openConnReply: make(chan messages.OpenConnectionReply),
		connections:   make(map[messages.ConnectionID]*Connection),
	}
	go c.inputDemux()
	return c
}

// Close closes the client.
func (c *Client) Close() error {
	return errors.Join(
		c.in.Close(),
		c.out.Close(),
	)
}

// NewConn creates a new ping connection.
func (c *Client) NewConn(ipVer util.IPVersion) (*Connection, error) {
	err := c.sendMessage(messages.OpenConnection{IPVer: ipVer})
	if err != nil {
		return nil, err
	}
	reply := <-c.openConnReply
	conn := &Connection{
		client: c,
		id:     reply.ID,
		// Buffered to prevent a "hold and wait" (possible deadlock) scenario,
		// since the send occurs while mu is locked.
		readFrom: make(chan messages.PingReply, 1),
		closed:   make(chan error, 1),
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connections[reply.ID] = conn
	return conn, nil
}

// Shutdown sends a shutdown message to the server.
func (c *Client) Shutdown() error {
	return c.sendMessage(messages.Shutdown{})
}

// Sends a message.
func (c *Client) sendMessage(msg messages.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := msg.WriteTo(c.out); err != nil {
		return fmt.Errorf("error writing to server: %v", err)
	}
	return nil
}

// Reads input from privsep server and sends it where it needs to go.
func (c *Client) inputDemux() {
	for {
		msg, err := messages.ReadMessage(c.inb)
		if err != nil {
			if !errors.Is(err, os.ErrClosed) {
				log.Printf("Error reading from privsep server: %v", err)
			}
			return
		}
		switch msg := msg.(type) {
		case messages.OpenConnectionReply:
			c.openConnReply <- msg
		case messages.CloseConnectionReply:
			c.handleCloseConnectionReply(msg)
		case messages.PingReply:
			c.handlePingReply(msg)
		default:
			log.Printf("Unknown message read from privsep server: %#v", msg)
		}
	}
}

func (c *Client) handleCloseConnectionReply(msg messages.CloseConnectionReply) {
	c.mu.Lock()
	defer c.mu.Unlock()
	conn, ok := c.connections[msg.ID]
	if !ok {
		log.Printf("Received close reply to already closed connection: %v", msg.ID)
		return
	}
	delete(c.connections, msg.ID)
	conn.closed <- nil
	conn.client = nil // Panic on future writes (reads will block infinitely)
}

func (c *Client) handlePingReply(msg messages.PingReply) {
	c.mu.Lock()
	defer c.mu.Unlock()
	conn, ok := c.connections[msg.ID]
	if !ok {
		log.Printf("Reply from unknown connection %v", msg.ID)
		return
	}
	conn.readFrom <- msg
}
