//go:build !rawsock

package icmpbase

import (
	"errors"
	"log"
	"net"
	"sync"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/util"
)

type icmpService struct {
	conn *internalConn
	sync.Mutex
	receiver chan<- readResult
}

func serviceFor(ipVer util.IPVersion) (*icmpService, error) {
	conn, err := newInternalConn(ipVer)
	if err != nil {
		return nil, err
	}
	return &icmpService{
		conn: conn,
	}, nil
}

func (s *icmpService) Close() error {
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
	s.Lock()
	defer s.Unlock()
	s.receiver <- readResult{
		Pkt:  pkt,
		Peer: peer,
		ID:   key.ID,
	}
}

func (s *icmpService) WriteTo(b []byte, peer net.Addr, opts ...backend.WriteOption) error {
	return s.conn.WriteTo(b, peer, opts...)
}

// Should only be called once on Linux since icmpService isn't a singleton.
func (s *icmpService) RegisterReader(id, proto int, receiver chan<- readResult) int {
	s.Lock()
	defer s.Unlock()
	if s.receiver != nil {
		log.Panicf("RegisterReader called twice; this is not how it should work on Linux.")
	}
	if id != 0 {
		log.Panicf("RegisterReader should have been called with 0 id on Linux. It must choose its own ICMP id.")
	}
	id = s.conn.echoID
	s.receiver = receiver
	go s.readLoop()
	return id
}

// Should only be called once on Linux since icmpService isn't a singleton.
func (s *icmpService) UnregisterReader(id, proto int) {
	s.Lock()
	defer s.Unlock()
	close(s.receiver)
}
