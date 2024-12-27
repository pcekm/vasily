package icmp

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/backend/icmpbase"
	"github.com/pcekm/graphping/internal/backend/test"
	"github.com/pcekm/graphping/internal/util"
)

var (
	localhostV4 = &net.UDPAddr{IP: net.ParseIP("127.0.0.1")}
	localhostV6 = &net.UDPAddr{IP: net.ParseIP("::1")}

	supportedOS = map[string]bool{
		"darwin": true,
		"linux":  true,
	}
)

// Returns a shallow copy of the given packet with Type set to PacketReply.
func asReply(pkt *backend.Packet) *backend.Packet {
	res := *pkt
	res.Type = backend.PacketReply
	return &res
}

func TestPingConnection(t *testing.T) {
	if !supportedOS[runtime.GOOS] && syscall.Getuid() != 0 {
		t.Skipf("Unsupported OS")
	}
	cases := []struct {
		ipVer      util.IPVersion
		listenAddr string
		dest       *net.UDPAddr
		ttl        int
	}{
		{ipVer: util.IPv4, dest: localhostV4},
		{ipVer: util.IPv4, dest: localhostV4, ttl: 1},
		{ipVer: util.IPv6, dest: localhostV6},
		{ipVer: util.IPv6, dest: localhostV6, ttl: 1},
	}
	for _, c := range cases {
		name := fmt.Sprintf("%s/%d", c.dest.IP.String(), c.ttl)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, err := baseNew(c.ipVer, icmpbase.NewUnlimited)
			if err != nil {
				t.Fatalf("Error opening connection: %v", err)
			}
			defer conn.Close()

			for seq := 0; seq < 10; seq++ {
				pkt := &backend.Packet{
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

				gotPkt, gotPeer, err := conn.ReadFrom(ctx)
				if err != nil {
					t.Errorf("ReadFrom error: %v", err)
				}
				if diff := cmp.Diff(asReply(pkt), gotPkt); diff != "" {
					t.Errorf("Wrong packet received (-want, +got):\n%v", diff)
				}

				if diff := test.DiffIP(c.dest, gotPeer); diff != "" {
					t.Errorf("Wrong response peer (-want, +got):\n%v", diff)
				}
			}
		})
	}
}

func TestConnectionCountLimit(t *testing.T) {
	if !supportedOS[runtime.GOOS] && syscall.Getuid() != 0 {
		t.Skipf("Unsupported OS")
	}

	const maxActiveConns = 100

	// First, create and close a connection, to ensure it doesn't continue to be
	// counted against the total.
	conn, err := New(util.IPv6)
	if err != nil {
		t.Fatalf("Error creating conn: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Error closing conn: %v", err)
	}

	// Open as many connections as allowed.
	for i := range maxActiveConns {
		conn, err := New(util.IPv4)
		if err != nil {
			t.Fatalf("Error creating conn %d: %v", i, err)
		}
		defer conn.Close()
	}

	// Try and hopefully fail to create one more.
	if conn, err := New(util.IPv4); err == nil {
		t.Errorf("No error creating connection %d", maxActiveConns+1)
		conn.Close()
	}
}
