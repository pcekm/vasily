package udp

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/backend/test"
	"github.com/pcekm/graphping/internal/util"
)

func TestWriteTo(t *testing.T) {

	cases := []struct {
		IPVer    util.IPVersion
		Dest     *net.UDPAddr
		WantType backend.PacketType
		WantPeer *net.UDPAddr
		TTL      int
	}{
		{
			IPVer:    util.IPv4,
			Dest:     test.LoopbackV4,
			WantType: backend.PacketReply,
			WantPeer: test.LoopbackV4,
		},
		{
			IPVer:    util.IPv4,
			Dest:     test.LoopbackV4,
			WantType: backend.PacketReply,
			WantPeer: test.LoopbackV4,
			TTL:      1,
		},
		{
			IPVer:    util.IPv6,
			Dest:     test.LoopbackV6,
			WantType: backend.PacketReply,
			WantPeer: test.LoopbackV6,
		},
		{
			IPVer:    util.IPv6,
			Dest:     test.LoopbackV6,
			WantType: backend.PacketReply,
			WantPeer: test.LoopbackV6,
			TTL:      1,
		},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%v/%v/%v", c.IPVer, c.Dest, c.TTL), func(t *testing.T) {
			// Running in parallel is part of the test. It ensures incoming ICMP packets
			// are handled by the correct connection.
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			conn, err := New(c.IPVer)
			if err != nil {
				t.Fatalf("Error opening conn: %v", err)
			}

			for seq := range 10 {
				pkt := &backend.Packet{Seq: seq}
				var opts []backend.WriteOption
				if c.TTL > 0 {
					opts = append(opts, backend.TTLOption{TTL: c.TTL})
				}
				if err := conn.WriteTo(pkt, c.Dest, opts...); err != nil {
					t.Errorf("WriteTo: %v", err)
				}

				got, peer, err := conn.ReadFrom(ctx)
				if err != nil {
					t.Errorf("ReadFrom: %v", err)
				}

				wantPkt := *pkt
				wantPkt.Type = c.WantType
				if diff := cmp.Diff(&wantPkt, got); diff != "" {
					t.Errorf("Wrong reply (-want, +got):\n%v", diff)
					if got != nil && len(got.Payload) > 0 {
						t.Errorf("Payload dump:\n%v", hex.Dump(got.Payload))
					}
				}

				if diff := cmp.Diff(c.WantPeer, peer); diff != "" {
					t.Errorf("Wrong peer (-want, +got):\n%v", diff)
				}
			}
		})
	}
}
