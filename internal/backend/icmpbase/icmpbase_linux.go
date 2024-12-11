//go:build !rawsock

package icmpbase

import (
	"log"
	"net"
	"os"

	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/net/icmp"
)

func newConn(ipVer util.IPVersion) (net.PacketConn, *os.File, error) {
	var network string
	switch ipVer {
	case util.IPv4:
		network = "ip4:icmp"
	case util.IPv6:
		network = "ip6:ipv6-icmp"
	default:
		log.Panicf("Unknown IP version: %v", ipVer)
	}
	conn, err := icmp.ListenPacket(network, "")
	return conn, nil, err
}

func wrangleAddr(addr net.Addr) *net.IPAddr {
	switch addr := addr.(type) {
	case *net.IPAddr:
		return addr
	case *net.UDPAddr:
		return &net.IPAddr{IP: addr.IP}
	}
	return nil
}

// Gets the ICMP id for this session.
func pingID(net.PacketConn) int {
	return util.GenID()
}

/*
type baseConn struct {
	sock int
}

func newBaseConn(addr *net.UDPAddr) (*baseConn, error) {
	sock, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_ICMP)
	if err != nil {
		return nil, fmt.Errorf("socket: %v", err)
	}

	// TODO: IPv6

	sa := syscall.SockaddrInet4{Addr: [4]byte(addr.IP.To4())}
	if err := syscall.Bind(sock, &sa); err != nil {
		return nil, fmt.Errorf("bind: %v", err)
	}

	// if err := syscall.SetsockoptInt(sock, syscall.SOL_IP, syscall.IP_TTL, 1); err != nil {
	// 	return nil, fmt.Errorf("setsockopt ttl: %v", err)
	// 	return
	// }

	if err := syscall.SetsockoptInt(sock, syscall.SOL_IP, syscall.IP_RECVERR, 1); err != nil {
		return nil, fmt.Errorf("setsockopt IP_RECVERR: %v", err)
	}

	// if err := syscall.Connect(s, &syscall.SockaddrInet4{Addr: [4]byte(net.ParseIP("142.251.46.206").To4())}); err != nil {
	// 	log.Printf("Connect: %v", err)
	// }

	// sn, err := syscall.Getsockname(sock)
	// if err != nil {
	// 	log.Printf("Getsockname: %v", err)
	// 	return
	// }
	// log.Printf("Sockname: %#v", sn)

	// buf := make([]byte, 1500)
	// oob := make([]byte, 1500)
	// for {
	// 	if err := sendMsg(sock, dest); err != nil {
	// 		log.Print(err)
	// 		time.Sleep(time.Second)
	// 		continue
	// 	}
	// 	n, oobn, rf, peer, err := syscall.Recvmsg(sock, buf, oob, syscall.MSG_ERRQUEUE)
	// 	if err != nil {
	// 		log.Printf("Recvmsg: %v", err)
	// 		time.Sleep(time.Second)
	// 		continue
	// 	}
	// 	log.Printf("Read: %x %v %v: %v (%v)", rf, peer, err, hex.EncodeToString(buf[:n]), hex.EncodeToString(oob[:oobn]))
	// 	time.Sleep(time.Second)
	// }
	//

	return &baseConn{
		sock: sock,
	}, nil
}

func saAddr(addr net.Addr) (syscall.Sockaddr, error) {
	switch addr := addr.(type) {
	case *net.UDPAddr:
		ip4 := addr.IP.To4()
		if ip4 == nil {
			return &syscall.SockaddrInet6{
				Port: addr.Port,
				// TODO: Convert zone str to int. Or better... get the standard
				// go library to work for Linux.
				// ZoneId: addr.Zone,
				Addr: [16]byte(addr.IP),
			}, nil
		}
		return &syscall.SockaddrInet4{Port: addr.Port, Addr: [4]byte(ip4)}, nil
	default:
		return nil, fmt.Errorf("unknown address type: %v", addr)
	}
}

func (c *baseConn) WriteTo(buf []byte, dest net.Addr) (n int, err error) {
	sa, err := saAddr(dest)
	if err != nil {
		return 0, err
	}
	return len(buf), syscall.Sendto(c.sock, buf, 0, sa)
}

func (c *baseConn) ReadFrom(buf []byte) (n int, addr net.Addr, err error) {
	var sa syscall.Sockaddr
	n, sa, err = syscall.Recvfrom(c.sock, buf, 0)
	if err != nil {
		return -1, nil, err
	}
	switch sa := sa.(type) {
	case *syscall.SockaddrInet4:
		addr = &net.UDPAddr{IP: sa.Addr[:], Port: sa.Port}
	}
	return n, addr, err
}

func (c *baseConn) Close() error {
	return syscall.Close(c.sock)
}

func (c *baseConn) LocalAddr() net.Addr {
	sn, err := syscall.Getsockname(c.sock)
	if err != nil {
		return nil
	}
	switch sn := sn.(type) {
	case *syscall.SockaddrInet4:
		return &net.UDPAddr{Port: sn.Port, IP: sn.Addr[:]}
	case *syscall.SockaddrInet6:
		// TODO: Zone?
		return &net.UDPAddr{Port: sn.Port, IP: sn.Addr[:]}
	default:
		return nil
	}
}
*/
