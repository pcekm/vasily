package icmppkt

import (
	"fmt"
	"net"
	"syscall"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// Makes an IP Header.
func ipHeader(t *testing.T, ipVer util.IPVersion, protocol int, payloadLen int) []byte {
	t.Helper()
	var iphBuf []byte
	var err error
	switch ipVer {
	case util.IPv4:
		iph := ipv4.Header{
			Version:  ipv4.Version,
			Len:      ipv4.HeaderLen,
			TotalLen: ipv4.HeaderLen + payloadLen,
			Protocol: protocol,
			Src:      net.IPv4(127, 0, 0, 1),
			Dst:      net.IPv4(127, 0, 0, 1),
		}
		iphBuf, err = iph.Marshal()
		if err != nil {
			t.Fatalf("IPHeader marshal: %v", err)
		}
	case util.IPv6:
		// The bare essentials. Sadly x/net/ipv6 doesn't do IP6 marshalling.
		// Fortunately the IPv6 header is much simpler than IPv4.
		iphBuf = make([]byte, ipv6.HeaderLen)
		iphBuf[0] = byte(6 << 4)
		iphBuf[4] = byte(payloadLen >> 8)
		iphBuf[5] = byte(payloadLen)
		iphBuf[6] = byte(protocol)
	}
	return iphBuf
}

// Makes an echo reply body for use as the data of an icmp error packet.
func echoReply(t *testing.T, ipVer util.IPVersion, id, seq int, payload []byte) []byte {
	t.Helper()
	var msgType icmp.Type
	switch ipVer {
	case util.IPv4:
		msgType = ipv4.ICMPTypeEcho
	case util.IPv6:
		msgType = ipv6.ICMPTypeEchoRequest
	default:
		t.Fatalf("Invalid ipVer: %v", ipVer)
	}

	msg := icmp.Message{
		Type: msgType,
		Body: &icmp.Echo{
			ID:   id,
			Seq:  seq,
			Data: payload,
		},
	}
	icmpBuf, err := msg.Marshal(nil)
	if err != nil {
		t.Fatalf("ICMP marshal: %v", err)
	}
	iphBuf := ipHeader(t, ipVer, ipVer.ICMPProtoNum(), len(icmpBuf))
	return append(iphBuf, icmpBuf...)
}

func udpPing(t *testing.T, ipVer util.IPVersion, id, seq int, payload []byte) []byte {
	t.Helper()

	udpPkt := util.UDPHeader{
		SrcPort:  uint16(id),
		DstPort:  uint16(seq),
		TotalLen: uint16(util.UDPHeaderLen + len(payload)),
	}
	udpBuf := udpPkt.Marshal(nil)
	iphBuf := ipHeader(t, ipVer, syscall.IPPROTO_UDP, len(udpBuf)+len(payload))
	res := append(iphBuf, udpBuf...)
	res = append(res, payload...)
	return res
}

func TestPackets(t *testing.T) {
	cases := []struct {
		Name string
		util.IPVersion
		In      *icmp.Message
		WantPkt *backend.Packet
		WantId  int
	}{
		{
			Name:      "ICMP/EchoRequest",
			IPVersion: util.IPv4,
			In:        &icmp.Message{Type: ipv4.ICMPTypeEcho, Body: &icmp.Echo{ID: 1, Seq: 2, Data: []byte{3, 4, 5}}},
			WantPkt:   &backend.Packet{Type: backend.PacketRequest, Seq: 2, Payload: []byte{3, 4, 5}},
			WantId:    1,
		},
		{
			Name:      "ICMP/EchoRequest",
			IPVersion: util.IPv6,
			In:        &icmp.Message{Type: ipv6.ICMPTypeEchoRequest, Body: &icmp.Echo{ID: 1, Seq: 2, Data: []byte{3, 4, 5}}},
			WantPkt:   &backend.Packet{Type: backend.PacketRequest, Seq: 2, Payload: []byte{3, 4, 5}},
			WantId:    1,
		},
		{
			Name:      "ICMP/EchoReply",
			IPVersion: util.IPv4,
			In:        &icmp.Message{Type: ipv4.ICMPTypeEchoReply, Body: &icmp.Echo{ID: 1, Seq: 2, Data: []byte{3, 4, 5}}},
			WantPkt:   &backend.Packet{Type: backend.PacketReply, Seq: 2, Payload: []byte{3, 4, 5}},
			WantId:    1,
		},
		{
			Name:      "ICMP/EchoReply",
			IPVersion: util.IPv6,
			In:        &icmp.Message{Type: ipv6.ICMPTypeEchoReply, Body: &icmp.Echo{ID: 1, Seq: 2, Data: []byte{3, 4, 5}}},
			WantPkt:   &backend.Packet{Type: backend.PacketReply, Seq: 2, Payload: []byte{3, 4, 5}},
			WantId:    1,
		},
		{
			Name:      "ICMP/TimeExceeded",
			IPVersion: util.IPv4,
			In:        &icmp.Message{Type: ipv4.ICMPTypeTimeExceeded, Body: &icmp.TimeExceeded{Data: echoReply(t, util.IPv4, 1, 2, []byte{3, 4, 5})}},
			WantPkt:   &backend.Packet{Type: backend.PacketTimeExceeded, Seq: 2, Payload: []byte{3, 4, 5}},
			WantId:    1,
		},
		{
			Name:      "ICMP/TimeExceeded",
			IPVersion: util.IPv6,
			In:        &icmp.Message{Type: ipv6.ICMPTypeTimeExceeded, Body: &icmp.TimeExceeded{Data: echoReply(t, util.IPv6, 1, 2, []byte{3, 4, 5})}},
			WantPkt:   &backend.Packet{Type: backend.PacketTimeExceeded, Seq: 2, Payload: []byte{3, 4, 5}},
			WantId:    1,
		},
		{
			Name:      "ICMP/DestinationUnreachable",
			IPVersion: util.IPv4,
			In:        &icmp.Message{Type: ipv4.ICMPTypeDestinationUnreachable, Body: &icmp.DstUnreach{Data: echoReply(t, util.IPv4, 1, 2, []byte{3, 4, 5})}},
			WantPkt:   &backend.Packet{Type: backend.PacketDestinationUnreachable, Seq: 2, Payload: []byte{3, 4, 5}},
			WantId:    1,
		},
		{
			Name:      "ICMP/DestinationUnreachable",
			IPVersion: util.IPv6,
			In:        &icmp.Message{Type: ipv6.ICMPTypeDestinationUnreachable, Body: &icmp.DstUnreach{Data: echoReply(t, util.IPv6, 1, 2, []byte{3, 4, 5})}},
			WantPkt:   &backend.Packet{Type: backend.PacketDestinationUnreachable, Seq: 2, Payload: []byte{3, 4, 5}},
			WantId:    1,
		},
		{
			Name:      "UDP/TimeExceeded",
			IPVersion: util.IPv4,
			In:        &icmp.Message{Type: ipv4.ICMPTypeTimeExceeded, Body: &icmp.TimeExceeded{Data: udpPing(t, util.IPv4, 1, 2, []byte{3, 4, 5})}},
			WantPkt:   &backend.Packet{Type: backend.PacketTimeExceeded, Seq: 2, Payload: []byte{3, 4, 5}},
			WantId:    1,
		},
		{
			Name:      "UDP/TimeExceeded",
			IPVersion: util.IPv6,
			In:        &icmp.Message{Type: ipv6.ICMPTypeTimeExceeded, Body: &icmp.TimeExceeded{Data: udpPing(t, util.IPv6, 1, 2, []byte{3, 4, 5})}},
			WantPkt:   &backend.Packet{Type: backend.PacketTimeExceeded, Seq: 2, Payload: []byte{3, 4, 5}},
			WantId:    1,
		},
		{
			Name:      "UDP/DestinationUnreachable",
			IPVersion: util.IPv4,
			In:        &icmp.Message{Type: ipv4.ICMPTypeDestinationUnreachable, Body: &icmp.DstUnreach{Data: udpPing(t, util.IPv4, 1, 2, []byte{3, 4, 5})}},
			WantPkt:   &backend.Packet{Type: backend.PacketDestinationUnreachable, Seq: 2, Payload: []byte{3, 4, 5}},
			WantId:    1,
		},
		{
			Name:      "UDP/DestinationUnreachable",
			IPVersion: util.IPv6,
			In:        &icmp.Message{Type: ipv6.ICMPTypeDestinationUnreachable, Body: &icmp.DstUnreach{Data: udpPing(t, util.IPv6, 1, 2, []byte{3, 4, 5})}},
			WantPkt:   &backend.Packet{Type: backend.PacketDestinationUnreachable, Seq: 2, Payload: []byte{3, 4, 5}},
			WantId:    1,
		},
		{
			Name:      "UDP/PortUnreachable",
			IPVersion: util.IPv4,
			In:        &icmp.Message{Type: ipv4.ICMPTypeDestinationUnreachable, Code: codePortUnreachableV4, Body: &icmp.DstUnreach{Data: udpPing(t, util.IPv4, 1, 2, []byte{3, 4, 5})}},
			WantPkt:   &backend.Packet{Type: backend.PacketReply, Seq: 2, Payload: []byte{3, 4, 5}},
			WantId:    1,
		},
		{
			Name:      "UDP/PortUnreachable",
			IPVersion: util.IPv6,
			In:        &icmp.Message{Type: ipv6.ICMPTypeDestinationUnreachable, Code: codePortUnreachableV6, Body: &icmp.DstUnreach{Data: udpPing(t, util.IPv6, 1, 2, []byte{3, 4, 5})}},
			WantPkt:   &backend.Packet{Type: backend.PacketReply, Seq: 2, Payload: []byte{3, 4, 5}},
			WantId:    1,
		},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%v/%v", c.Name, c.IPVersion), func(t *testing.T) {
			buf, err := c.In.Marshal(nil)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}
			got, id, err := Parse(c.IPVersion, buf)
			if err != nil {
				t.Fatalf("Conversion error: %v", err)
			}
			if diff := cmp.Diff(c.WantPkt, got); diff != "" {
				t.Errorf("Wrong packet (-want, +got):\n%v", diff)
			}
			if id != c.WantId {
				t.Errorf("Wrong id: %d (want %d)", id, c.WantId)
			}
		})
	}

}
