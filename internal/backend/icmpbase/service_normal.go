//go:build rawsock || !linux

package icmpbase

import (
	"errors"
	"log"
	"net"
	"sync"

	"github.com/pcekm/vasily/internal/backend"
	"github.com/pcekm/vasily/internal/util"
)

var (
	serviceStart sync.Once
	serviceV4    *icmpService
	serviceV6    *icmpService
)

func serviceFor(ipVer util.IPVersion) (*icmpService, error) {
	maybeStartService()
	switch ipVer {
	case util.IPv4:
		return serviceV4, nil
	case util.IPv6:
		return serviceV6, nil
	default:
		log.Panicf("Unknown IP version: %v", ipVer)
	}
	return nil, errors.New("unreachable case")
}

func maybeStartService() {
	serviceStart.Do(func() {
		var err error
		serviceV4, err = newICMPService(util.IPv4)
		if err != nil {
			log.Panicf("Error starting ICMPv4 service: %v", err)
		}
		serviceV6, err = newICMPService(util.IPv6)
		if err != nil {
			log.Panicf("Error starting ICMPv6 service: %v", err)
		}
	})
}

type icmpService struct {
	ipVer util.IPVersion
	conn  *internalConn
	done  chan struct{}

	listeners sync.Map
}

func newICMPService(ipVer util.IPVersion) (*icmpService, error) {
	conn, err := newInternalConn(ipVer)
	if err != nil {
		return nil, err
	}
	s := &icmpService{
		ipVer: ipVer,
		conn:  conn,
		done:  make(chan struct{}),
	}
	go s.readLoop()
	return s, nil
}

func (s *icmpService) Close() error {
	close(s.done)
	return s.conn.Close()
}

func (s *icmpService) readLoop() {
	for {
		pkt, peer, key, err := s.conn.ReadFrom()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("Read error: %v", err)
			return
		}
		go s.sendToReceiver(pkt, peer, key)
	}
}

func (s *icmpService) sendToReceiver(pkt *backend.Packet, peer net.Addr, key listenerKey) {
	// Filter sent ICMPV6 echo requests that are also received on the same
	// connection. (Mostly a problem for unprivileged ICMP on macOS.)
	if pkt.Type == backend.PacketRequest {
		return
	}

	rcvr, ok := s.listeners.Load(key)
	if !ok {
		return
	}

	rcvr.(chan<- readResult) <- readResult{
		Pkt:  pkt,
		Peer: peer,
		ID:   key.ID,
	}
}

func (s *icmpService) WriteTo(b []byte, peer net.Addr, opts ...backend.WriteOption) error {
	return s.conn.WriteTo(b, peer, opts...)
}

// RegisterReader registers a receiver for the given id and protocol number. If
// ID is 0, an ID will be assigned. The provided or assigned id will be
// returned.
func (s *icmpService) RegisterReader(id, proto int, receiver chan<- readResult) int {
	if id == 0 {
		id = util.GenID()
	}
	s.listeners.Store(listenerKey{ID: id, Proto: proto}, receiver)
	return id
}

func (s *icmpService) UnregisterReader(id, proto int) {
	key := listenerKey{ID: id, Proto: proto}
	rcvr, ok := s.listeners.LoadAndDelete(key)
	if !ok {
		return
	}
	close(rcvr.(chan<- readResult))
}
