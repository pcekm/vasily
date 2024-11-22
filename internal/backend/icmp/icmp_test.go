package icmp

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/ping/util"
)

var (
	localhostV4 = &net.UDPAddr{IP: net.ParseIP("127.0.0.1")}
	localhostV6 = &net.UDPAddr{IP: net.ParseIP("::1")}
)

// Returns a shallow copy of the given packet with Type set to PacketReply.
func asReply(pkt *backend.Packet) *backend.Packet {
	res := *pkt
	res.Type = backend.PacketReply
	return &res
}

func TestPingConnection(t *testing.T) {
	cases := []struct {
		network    string
		listenAddr string
		dest       *net.UDPAddr
		ttl        int
	}{
		{network: "udp4", dest: localhostV4},
		{network: "udp4", dest: localhostV4, ttl: 1},
		{network: "udp4", dest: localhostV4, listenAddr: localhostV4.IP.String()},
		{network: "udp4", dest: localhostV4, listenAddr: localhostV4.IP.String(), ttl: 1},
		{network: "udp6", dest: localhostV6},
		{network: "udp6", dest: localhostV6, ttl: 1},
		{network: "udp6", dest: localhostV6, listenAddr: localhostV6.IP.String()},
		{network: "udp6", dest: localhostV6, listenAddr: localhostV6.IP.String(), ttl: 1},
	}
	for _, c := range cases {
		name := fmt.Sprintf("%s/%s/%d", c.dest.IP.String(), c.listenAddr, c.ttl)
		t.Run(name, func(t *testing.T) {
			conn, err := New(c.network, c.listenAddr)
			if err != nil {
				t.Fatalf("Error opening connection: %v", err)
			}

			if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Errorf("Error setting deadline: %v", err)
			}

			id := util.GenID()
			for seq := 0; seq < 10; seq++ {
				pkt := &backend.Packet{
					ID:      id,
					Seq:     seq,
					Payload: []byte("the payload"),
				}
				opts := []backend.WriteOption{}
				if c.ttl != 0 {
					opts = append(opts, backend.TTLOption{TTL: c.ttl})
				}

				if err := conn.WriteTo(pkt, c.dest, opts...); err != nil {
					t.Fatalf("WriteTo error: %v", err)
				}

				gotPkt, gotPeer, err := conn.ReadFrom()
				if err != nil {
					t.Errorf("ReadFrom error: %v", err)
				}
				if diff := cmp.Diff(asReply(pkt), gotPkt); diff != "" {
					t.Errorf("Wrong packet received (-want, +got):\n%v", diff)
				}

				if diff := cmp.Diff(c.dest, gotPeer); diff != "" {
					t.Errorf("Wrong response peer (-want, +got):\n%v", diff)
				}
			}
		})
	}
}
