package privsep

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/pcekm/vasily/internal/backend"
	"github.com/pcekm/vasily/internal/backend/icmp"
	"github.com/pcekm/vasily/internal/privsep/messages"
	"github.com/pcekm/vasily/internal/util"
)

type connMaker func() *icmp.PingConn

func defaultNewIPv4Conn() *icmp.PingConn {
	conn, err := icmp.New(util.IPv4)
	if err != nil {
		log.Panicf("Error opening IPv4 connection: %v", err)
	}
	return conn
}

func defaultNewIPv6Conn() *icmp.PingConn {
	conn, err := icmp.New(util.IPv6)
	if err != nil {
		log.Panicf("Error opening IPv4 connection: %v", err)
	}
	return conn
}

// Handles messages from [privClient] and issues replies.
type Server struct {
	osExit func(int) // For test injection
	conns  map[messages.ConnectionID]backend.Conn
	nextId messages.ConnectionID

	in *os.File

	mu  sync.Mutex
	out *os.File
}

func newServer() *Server {
	return &Server{
		in:     os.Stdin,
		out:    os.Stdout,
		osExit: os.Exit,
		conns:  make(map[messages.ConnectionID]backend.Conn),
	}
}

// Runs the server and blocks forever.
func (s *Server) run() {
	r := bufio.NewReader(s.in)
	for {
		msg, err := messages.ReadMessage(r)
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			log.Fatalf("ReadMessage error: %v", err)
		}
		s.handleMessage(msg)
	}
}

// Reads from connection in a loop. Exits when the connection is closed.
func (s *Server) readLoop(id messages.ConnectionID) {
	conn := s.connFor(id)
	for {
		pkt, peer, err := conn.ReadFrom(context.TODO())
		if err != nil {
			// This is less than ideal. It would be nice if the error was a
			// net.ErrClosed, but it isn't. So we have to check the error
			// message. At least if the message ever changes it'll fail
			// spectacularly, and be easy to find.
			if strings.Contains(err.Error(), "closed network connection") {
				return
			}
			log.Panicf("Error reading from connection: %v", err)
		}
		msg := messages.PingReply{
			ID:     id,
			Packet: *pkt,
			Peer:   util.IP(peer),
		}
		s.write(msg)
	}
}

// Closes the server. This is meant for tests, and therefore doesn't exit the
// process.
func (s *Server) Close() error {
	var errs []error
	for _, conn := range s.conns {
		err := conn.Close()
		if err != nil {
			errs = append(errs, err)
		}
	}
	if err := s.in.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := s.out.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (s *Server) connFor(id messages.ConnectionID) backend.Conn {
	conn, ok := s.conns[id]
	if !ok {
		log.Panicf("No ICMP connection for %d", id)
	}
	return conn
}

// Writes a message to the client. Panics on error.
func (s *Server) write(msg messages.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := msg.WriteTo(s.out)
	if err != nil {
		log.Panicf("Error writing message: %v", err)
	}
}

func (s *Server) handleMessage(msg messages.Message) {
	switch msg := msg.(type) {
	case messages.Shutdown:
		s.handleShutdown(msg)
	case messages.PrivilegeDrop:
		s.handlePrivilegeDrop(msg)
	case messages.OpenConnection:
		s.handleOpenConnection(msg)
	case messages.OpenConnectionReply:
		s.handleOpenConnectionReply(msg)
	case messages.CloseConnection:
		s.handleCloseConnection(msg)
	case messages.SendPing:
		s.handleSendPing(msg)
	case messages.PingReply:
		s.handlePingReply(msg)
	default:
		log.Panicf("Invalid message: %v", msg)
	}
}

func (s *Server) handleShutdown(messages.Shutdown) {
	s.osExit(0)
}

func (s *Server) handlePrivilegeDrop(messages.PrivilegeDrop) {
	if err := dropPrivileges(); err != nil {
		log.Panicf("Failed to drop privileges: %v", err)
	}
}

func (s *Server) handleOpenConnection(msg messages.OpenConnection) {
	conn, err := backend.New(msg.Backend, msg.IPVer)
	if err != nil {
		log.Panicf("Error opening connection: %v", err)
	}
	id := s.nextId
	s.nextId++
	s.conns[id] = conn
	go s.readLoop(id)
	s.write(messages.OpenConnectionReply{
		ID: id,
	})
}

func (s *Server) handleOpenConnectionReply(msg messages.OpenConnectionReply) {
	log.Panicf("Unexpected message: %v", msg)
}

func (s *Server) handleCloseConnection(msg messages.CloseConnection) {
	conn := s.connFor(msg.ID)
	if err := conn.Close(); err != nil {
		log.Panicf("Error closing connection: %v", err)
	}
	delete(s.conns, msg.ID)
}

func (s *Server) handleSendPing(msg messages.SendPing) {
	conn := s.connFor(msg.ID)
	var opts []backend.WriteOption
	if msg.TTL != 0 {
		opts = append(opts, backend.TTLOption{TTL: msg.TTL})
	}
	if err := conn.WriteTo(&msg.Packet, &net.UDPAddr{IP: msg.Addr}, opts...); err != nil {
		log.Panicf("Error sending ping: %v", err)
	}
}

func (s *Server) handlePingReply(msg messages.PingReply) {
	log.Panicf("Unexpected message: %v", msg)
}
