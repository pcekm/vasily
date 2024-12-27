package icmpbase

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/backend/test"
	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const payload = "Give me a ping, Vasily. One ping only, please."

var (
	// Reserved for documentation; shouldn't ever reply.
	badAddrV4 = &net.UDPAddr{IP: net.ParseIP("192.0.2.1")}
	// IPv6 is problematic. The Github workflow runners don't seem to have
	// anything other than the loopback interface. Which means that writes to
	// anything other than ::1 will immediately fail with "no route to host."
	// This can be fixed by expanding the loopback network a bit. For example,
	// on Linux:
	//
	//   ifconfig lo inet6 del ::1 add inet6 ::1/126
	//
	// Now a write to ::2 won't immediately fail. (It still won't respond, but
	// that's what the test needs.)
	badAddrV6 = &net.UDPAddr{IP: net.ParseIP("::2")}

	supportedOS = map[string]bool{
		"darwin": true,
		"linux":  true,
	}
)

// Returns a shallow copy of the given packet with Type set to PacketReply.
func asReply(msg *icmp.Message) *backend.Packet {
	body := msg.Body.(*icmp.Echo)
	res := &backend.Packet{
		Type:    backend.PacketReply,
		Seq:     body.Seq,
		Payload: body.Data,
	}
	return res
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
	if !supportedOS[runtime.GOOS] && syscall.Getuid() != 0 {
		t.Skipf("Unsupported OS")
	}
	cases := []struct {
		ipVer       util.IPVersion
		listenAddr  string
		dest        *net.UDPAddr
		ttl         int
		wantTimeout bool
	}{
		{ipVer: util.IPv4, dest: test.LoopbackV4},
		{ipVer: util.IPv4, dest: test.LoopbackV4, ttl: 1},
		{ipVer: util.IPv4, dest: badAddrV4, wantTimeout: true},
		{ipVer: util.IPv6, dest: test.LoopbackV6},
		{ipVer: util.IPv6, dest: test.LoopbackV6, ttl: 1},
		{ipVer: util.IPv6, dest: badAddrV6, wantTimeout: true},
	}
	for _, c := range cases {
		name := fmt.Sprintf("%s/%d", c.dest.IP.String(), c.ttl)
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			conn, err := NewUnlimited(c.ipVer, 0, c.ipVer.ICMPProtoNum())
			if err != nil {
				t.Fatalf("Error opening connection: %v", err)
			}
			defer conn.Close()

			reqType := util.Choose[icmp.Type](c.ipVer, ipv4.ICMPTypeEcho, ipv6.ICMPTypeEchoRequest)

			// Messages may be rate limited by the OS. Don't iterate too many times here.
			for seq := 0; seq < 3; seq++ {
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

				gotMsg, gotPeer, err := conn.ReadFrom(ctx)
				if !c.wantTimeout && err != nil {
					t.Fatalf("ReadFrom seq %d error: %v", seq, err)
				} else if c.wantTimeout && !errors.Is(err, backend.ErrTimeout) {
					t.Fatalf("ReadFrom seq %d wanted timeout err (got %v)", seq, err)
				}
				if !c.wantTimeout {
					want := asReply(msg)
					if diff := cmp.Diff(want, gotMsg); diff != "" {
						t.Errorf("Wrong packet received (-want, +got):\n%v", diff)
					}
					if diff := test.DiffIP(c.dest, gotPeer); diff != "" {
						t.Errorf("Wrong response peer (-want, +got):\n%v", diff)
					}
				}
			}
		})
	}
}

func TestConnectionCountLimit(t *testing.T) {
	if !supportedOS[runtime.GOOS] && syscall.Getuid() != 0 {
		t.Skipf("Unsupported OS")
	}

	// First, create and close a connection, to ensure it doesn't continue to be
	// counted against the total.
	conn, err := New(util.IPv6, 0, util.IPv6.ICMPProtoNum())
	if err != nil {
		t.Fatalf("Error creating conn: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Error closing conn: %v", err)
	}

	// Open as many connections as allowed.
	for i := range maxActiveConns {
		conn, err := New(util.IPv4, 0, util.IPv4.ICMPProtoNum())
		if err != nil {
			t.Fatalf("Error creating conn %d: %v", i, err)
		}
		defer conn.Close()
	}

	// Try and hopefully fail to create one more.
	if conn, err := New(util.IPv4, 0, util.IPv4.ICMPProtoNum()); err == nil {
		t.Errorf("No error creating connection %d", maxActiveConns+1)
		conn.Close()
	}
}
