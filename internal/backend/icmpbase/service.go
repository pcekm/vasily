package icmpbase

import (
	"net"

	"github.com/pcekm/graphping/internal/backend"
)

type readResult struct {
	Pkt  *backend.Packet
	Peer net.Addr
	ID   int
}

type listenerKey struct {
	ID    int
	Proto int
}
