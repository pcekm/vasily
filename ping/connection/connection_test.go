package connection

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/graphping/ping/util"
)

var (
	localhostV4 = &net.UDPAddr{IP: net.ParseIP("127.0.0.1")}
	localhostV6 = &net.UDPAddr{IP: net.ParseIP("::1")}
)

type result struct {
	Pkt  *Packet
	Peer net.Addr
}

// Returns a shallow copy of the given packet with Type set to PacketReply.
func asReply(pkt *Packet) *Packet {
	res := *pkt
	res.Type = PacketReply
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
			res := make(chan result)
			callback := func(pkt *Packet, peer net.Addr) {
				res <- result{Pkt: pkt, Peer: peer}
			}
			defer conn.AddCallback(callback)()

			id := util.GenID()
			for seq := 0; seq < 10; seq++ {
				pkt := &Packet{
					ID:      id,
					Seq:     seq,
					Payload: []byte("the payload"),
				}
				var err error
				if c.ttl == 0 {
					err = conn.WriteTo(pkt, c.dest)
				} else {
					err = conn.WriteToTTL(pkt, c.dest, c.ttl)
				}
				if err != nil {
					t.Fatalf("Write error: %v", err)
				}
				want := result{
					Pkt:  asReply(pkt),
					Peer: c.dest,
				}

				var got result
				timeout := time.After(5 * time.Second)
			loop:
				for {
					select {
					case got = <-res:
						if got.Pkt.ID == id && got.Pkt.Seq == seq {
							break loop
						}
					case <-timeout:
						t.Errorf("Timed out waiting for ID=%d Seq=%d", id, seq)
						break loop
					}
				}

				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("Wrong ping response (-want, +got):\n%v", diff)
				}
			}
		})
	}
}
