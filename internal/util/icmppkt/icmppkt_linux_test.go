package icmppkt

import (
	"net"
	"runtime"
	"testing"

	"github.com/pcekm/vasily/internal/backend"
	"github.com/pcekm/vasily/internal/util"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.org/x/sys/unix"
)

func TestOOBBytes(t *testing.T) {
	oob4 := OOBBytes(util.IPv4)
	if len(oob4) == 0 {
		t.Errorf("OOBBytes didn't allocate any memory for IPv4.")
	}
	oob6 := OOBBytes(util.IPv6)
	if len(oob6) == 0 {
		t.Errorf("OOBBytes didn't allocate any memory for IPv6.")
	}
}

func makeOOB(origin byte, typ icmp.Type, code byte) []byte {
	switch typ := typ.(type) {
	case ipv4.ICMPType:
		return []byte{
			0x30, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0b, 0x00, 0x00, 0x00,
			0x71, 0x00, 0x00, 0x00, origin, byte(typ), code, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x02, 0x00, 0x00, 0x00, 0x8e, 0xfb, 0xe0, 0xaf, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		}
	case ipv6.ICMPType:
		return []byte{
			0x3c, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x29, 0x00, 0x00, 0x00, 0x19, 0x00, 0x00, 0x00,
			0x71, 0x00, 0x00, 0x00, origin, byte(typ), code, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20, 0x01, 0x05, 0x58, 0x10, 0x14, 0x6e, 0x3c,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		}
	}
	return nil
}

// TODO: This test is brittle. It assumes a specific C struct layout, which may
// or may not work on different hardware. (It definitely _won't_ work on 32-bit
// or big-endian machines.
func TestParseLinuxEE(t *testing.T) {
	if runtime.GOARCH != "arm64" && runtime.GOARCH != "amd64" {
		t.Skip("Unsupported CPU.")
	}
	cases := []struct {
		Name     string
		In       []byte
		WantType backend.PacketType
		WantAddr net.IP
	}{
		{
			Name:     "TimeExceeded/IPv4",
			In:       makeOOB(unix.SO_EE_ORIGIN_ICMP, ipv4.ICMPTypeTimeExceeded, 0),
			WantType: backend.PacketTimeExceeded,
			WantAddr: net.ParseIP("142.251.224.175"),
		},
		{
			Name:     "TimeExceeded/IPv6",
			In:       makeOOB(unix.SO_EE_ORIGIN_ICMP6, ipv6.ICMPTypeTimeExceeded, 0),
			WantType: backend.PacketTimeExceeded,
			WantAddr: net.ParseIP("2001:558:1014:6e3c::2"),
		},
		{
			Name:     "PortUnreachable/IPv4",
			In:       makeOOB(unix.SO_EE_ORIGIN_ICMP, ipv4.ICMPTypeDestinationUnreachable, codePortUnreachableV4),
			WantType: backend.PacketReply,
			WantAddr: net.ParseIP("142.251.224.175"),
		},
		{
			Name:     "PortUnreachable/IPv6",
			In:       makeOOB(unix.SO_EE_ORIGIN_ICMP6, ipv6.ICMPTypeDestinationUnreachable, codePortUnreachableV6),
			WantType: backend.PacketReply,
			WantAddr: net.ParseIP("2001:558:1014:6e3c::2"),
		},
		{
			Name:     "HostUnreachable/IPv4",
			In:       makeOOB(unix.SO_EE_ORIGIN_ICMP, ipv4.ICMPTypeDestinationUnreachable, 1),
			WantType: backend.PacketDestinationUnreachable,
			WantAddr: net.ParseIP("142.251.224.175"),
		},
		{
			Name:     "HostUnreachable/IPv6",
			In:       makeOOB(unix.SO_EE_ORIGIN_ICMP6, ipv6.ICMPTypeDestinationUnreachable, 3),
			WantType: backend.PacketDestinationUnreachable,
			WantAddr: net.ParseIP("2001:558:1014:6e3c::2"),
		},
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			pktType, peer, err := ParseLinuxEE(c.In)
			if err != nil {
				t.Fatalf("ParseLinuxEE error: %v", err)
			}
			if pktType != c.WantType {
				t.Errorf("Wrong packet type: %v (want %v)", pktType, c.WantType)
			}
			if !util.IP(peer).Equal(c.WantAddr) {
				t.Errorf("Wrong address: %v (want %v)", peer, c.WantAddr)
			}
		})
	}
}
