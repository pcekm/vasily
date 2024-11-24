package privsep

import (
	"bufio"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/pcekm/graphping/internal/backend/icmp"
)

type connMaker func() *icmp.PingConn

func defaultNewIPv4Conn() *icmp.PingConn {
	net := "ip4:1"
	if runtime.GOOS == "darwin" {
		net = "udp4"
	}
	conn, err := icmp.New(net, "")
	if err != nil {
		log.Panicf("Error opening IPv4 connection: %v", err)
	}
	return conn
}

func defaultNewIPv6Conn() *icmp.PingConn {
	net := "ip6:58"
	if runtime.GOOS == "darwin" {
		net = "udp6"
	}
	conn, err := icmp.New(net, "")
	if err != nil {
		log.Panicf("Error opening IPv4 connection: %v", err)
	}
	return conn
}

// Handles messages from [privClient] and issues replies.
type privServer struct {
	newIPv4 connMaker
	newIPv6 connMaker
	osExit  func(int) // For test injection
	conns   map[ConnectionID]*icmp.PingConn
	nextId  ConnectionID

	in *os.File

	mu  sync.Mutex
	out *os.File
}

func newServer() *privServer {
	return &privServer{
		in:      os.Stdin,
		out:     os.Stdout,
		newIPv4: defaultNewIPv4Conn,
		newIPv6: defaultNewIPv6Conn,
		osExit:  os.Exit,
		conns:   make(map[ConnectionID]*icmp.PingConn),
	}
}

// Runs the server and blocks forever.
func (s *privServer) run() {
	r := bufio.NewReader(s.in)
	for {
		msg, err := ReadMessage(r)
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
func (s *privServer) readLoop(id ConnectionID) {
	conn := s.connFor(id)
	for {
		pkt, peer, err := conn.ReadFrom()
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
		msg := PingReply{
			ID:     id,
			Packet: *pkt,
			Peer:   peer.(*net.UDPAddr).IP,
		}
		s.write(msg)
	}
}

// Closes the server. This is meant for tests, and therefore doesn't exit the
// process.
func (s *privServer) Close() error {
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

func (s *privServer) connFor(id ConnectionID) *icmp.PingConn {
	conn, ok := s.conns[id]
	if !ok {
		log.Panicf("No ICMP connection for %d", id)
	}
	return conn
}

// Writes a message to the client. Panics on error.
func (s *privServer) write(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := msg.WriteTo(s.out)
	if err != nil {
		log.Panicf("Error writing message: %v", err)
	}
}

func (s *privServer) handleMessage(msg Message) {
	switch msg := msg.(type) {
	case Shutdown:
		s.handleShutdown(msg)
	case PrivilegeDrop:
		s.handlePrivilegeDrop(msg)
	case OpenConnection:
		s.handleOpenConnection(msg)
	case OpenConnectionReply:
		s.handleOpenConnectionReply(msg)
	case CloseConnection:
		s.handleCloseConnection(msg)
	case SendPing:
		s.handleSendPing(msg)
	case PingReply:
		s.handlePingReply(msg)
	default:
		log.Panicf("Invalid message: %v", msg)
	}
}

func (s *privServer) handleShutdown(Shutdown) {
	s.osExit(0)
}

func (s *privServer) handlePrivilegeDrop(PrivilegeDrop) {
	if err := dropPrivileges(); err != nil {
		log.Panicf("Failed to drop privileges: %v", err)
	}
}

func (s *privServer) handleOpenConnection(msg OpenConnection) {
	var conn *icmp.PingConn
	switch msg.IPVer {
	case IPv4:
		conn = s.newIPv4()
	case IPv6:
		conn = s.newIPv6()
	default:
		log.Panicf("Unknown IP version: %v", msg.IPVer)
	}
	id := s.nextId
	s.nextId++
	s.conns[id] = conn
	go s.readLoop(id)
	s.write(OpenConnectionReply{
		ID: id,
	})
}

func (s *privServer) handleOpenConnectionReply(msg OpenConnectionReply) {
	log.Panicf("Unexpected message: %v", msg)
}

func (s *privServer) handleCloseConnection(msg CloseConnection) {
	conn := s.connFor(msg.ID)
	if err := conn.Close(); err != nil {
		log.Panicf("Error closing connection: %v", err)
	}
	delete(s.conns, msg.ID)
}

func (s *privServer) handleSendPing(msg SendPing) {
	conn := s.connFor(msg.ID)
	if err := conn.WriteTo(&msg.Packet, &net.UDPAddr{IP: msg.Addr}); err != nil {
		log.Panicf("Error sending ping: %v", err)
	}
}

func (s *privServer) handlePingReply(msg PingReply) {
	log.Panicf("Unexpected message: %v", msg)
}
