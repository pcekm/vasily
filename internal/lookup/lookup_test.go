package lookup

import (
	"net"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestAddr(t *testing.T) {
	const lh = "localhost"
	lhv4 := net.ParseIP("127.0.0.1")
	lhv6 := net.ParseIP("::1")
	cases := []struct {
		addr net.Addr
		want string
	}{
		{addr: &net.IPAddr{IP: lhv4}, want: lh},
		{addr: &net.UDPAddr{IP: lhv4}, want: lh},
		{addr: &net.TCPAddr{IP: lhv4}, want: lh},
		{addr: &net.IPAddr{IP: lhv6}, want: lh},
		{addr: &net.UDPAddr{IP: lhv6}, want: lh},
		{addr: &net.TCPAddr{IP: lhv6}, want: lh},
		{addr: &net.IPAddr{IP: net.ParseIP("192.0.2.1")}, want: "192.0.2.1"}, // Dedicated example IP should never resolve
		{addr: &net.UnixAddr{Name: "/tmp/unix.sock"}, want: "/tmp/unix.sock"},
	}
	for _, c := range cases {
		t.Run(c.addr.String(), func(t *testing.T) {
			t.Parallel()
			if got := Addr(c.addr); c.want != got {
				t.Errorf("Wrong name for address: want %v got %v", c.want, got)
			}
		})
	}
}

func TestString(t *testing.T) {
	cases := []struct {
		s    string
		want *net.UDPAddr
	}{
		{s: "127.0.0.1", want: &net.UDPAddr{IP: net.ParseIP("127.0.0.1")}},
		{s: "::1", want: &net.UDPAddr{IP: net.ParseIP("::1")}},
		{s: "192.0.2.1", want: &net.UDPAddr{IP: net.ParseIP("192.0.2.1")}},
		{s: "localhost", want: &net.UDPAddr{IP: net.ParseIP("127.0.0.1")}},
	}
	for _, c := range cases {
		t.Run(c.s, func(t *testing.T) {
			t.Parallel()
			addr, err := String(c.s)
			if err != nil {
				t.Fatalf("Error looking up name: %v", err)
			}
			if diff := cmp.Diff(c.want, addr); diff != "" {
				t.Errorf("Wrong address (-want, +got):\n%v", diff)
			}
		})
	}
}
