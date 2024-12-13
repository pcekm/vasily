//go:build darwin

package icmpbase

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.org/x/time/rate"
)

const payload = "Give me a ping, Vasily. One ping only, please."

var (
	localhostV4 = &net.UDPAddr{IP: net.ParseIP("127.0.0.1")}
	localhostV6 = &net.UDPAddr{IP: net.ParseIP("::1")}
)

// Returns a shallow copy of the given packet with Type set to PacketReply.
func asReply(msg *icmp.Message) *icmp.Message {
	res := *msg
	switch res.Type {
	case ipv4.ICMPTypeEcho:
		res.Type = ipv4.ICMPTypeEchoReply
	case ipv6.ICMPTypeEchoRequest:
		res.Type = ipv6.ICMPTypeEchoReply
	}
	return &res
}

func marshal(t *testing.T, msg *icmp.Message) []byte {
	t.Helper()
	buf, err := msg.Marshal(nil)
	if err != nil {
		t.Fatalf("Error marshalling message: %#v", msg)
	}
	return buf
}

func unmarshal(t *testing.T, ipVer util.IPVersion, buf []byte) *icmp.Message {
	t.Helper()
	msg, err := icmp.ParseMessage(ipVer.ICMPProtoNum(), buf)
	if err != nil {
		t.Fatalf("Error unmarshaling message: %v", err)
	}
	return msg
}

func TestPingConnection(t *testing.T) {
	if runtime.GOOS != "darwin" {
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
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			conn, err := New(c.ipVer)
			if err != nil {
				t.Fatalf("Error opening connection: %v", err)
			}
			defer conn.Close()
			conn.limiter.SetLimit(rate.Inf)

			var reqType, replType icmp.Type
			switch c.ipVer {
			case util.IPv4:
				reqType = ipv4.ICMPTypeEcho
				replType = ipv4.ICMPTypeEchoReply
			case util.IPv6:
				reqType = ipv6.ICMPTypeEchoRequest
				replType = ipv6.ICMPTypeEchoReply
			}

			for seq := 0; seq < 10; seq++ {
				msg := &icmp.Message{
					Type: reqType,
					Body: &icmp.Echo{ID: conn.EchoID(), Seq: seq, Data: []byte(payload)},
				}
				opts := []backend.WriteOption{}
				if c.ttl != 0 {
					opts = append(opts, backend.TTLOption{TTL: c.ttl})
				}

				if err := conn.WriteTo(marshal(t, msg), c.dest, opts...); err != nil {
					t.Fatalf("WriteTo error: %v", err)
				}

				var (
					gotMsg  *icmp.Message
					gotPeer net.Addr
					n       int
					buf     = make([]byte, maxMTU)
				)
				for ctx.Err() == nil {
					n, gotPeer, err = conn.ReadFrom(ctx, buf)
					if err != nil {
						t.Fatalf("ReadFrom error: %v", err)
					}
					gotMsg = unmarshal(t, c.ipVer, buf[:n])
					if gotMsg.Type != replType || gotMsg.Body.(*icmp.Echo).ID != conn.EchoID() {
						continue
					}
					break
				}
				gotMsg.Checksum = 0
				if diff := cmp.Diff(asReply(msg), gotMsg); diff != "" {
					t.Errorf("Wrong packet received (-want, +got):\n%v", diff)
				}

				if diff := cmp.Diff(c.dest, gotPeer); diff != "" {
					t.Errorf("Wrong response peer (-want, +got):\n%v", diff)
				}
			}
		})
	}
}

func TestConnectionCountLimit(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skipf("Unsupported OS")
	}

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
