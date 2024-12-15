package icmppkt

/*
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <linux/errqueue.h>
#include <netdb.h>

// Wraps the SO_EE_OFFENDER macro in a function so it can be called from Go
// code.
struct sockaddr* so_ee_offender(struct sock_extended_err *ee) {
	return SO_EE_OFFENDER(ee);
}
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"unsafe"

	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/util"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.org/x/sys/unix"
)

// OOBBytes allocates enough bytes to fit a struct msghdr, struct
// sock_extended_err, and struct sockaddr returned in the oob field of
// [unix.Recvmsg].
func OOBBytes(ipVer util.IPVersion) []byte {
	saSize := util.Choose(ipVer, C.sizeof_struct_sockaddr_in, C.sizeof_struct_sockaddr_in6)
	return make([]byte, unix.CmsgSpace(int(C.sizeof_struct_sock_extended_err+saSize)))
}

// ParseLinuxEE parses a linux struct sock_extended_err obtained with the
// MSG_ERRQUEUE flag.
//
// Example:
//
//	buf := make([]byte, 1500)
//	oob := OOBBytes(util.IPv4)
//	n, oobn, _, _ err := unix.Recvmsg(fd, buf, oob, unix.MSG_ERRQUEUE)
//	packet, peer, err := ParseLinuxEE(util.IPv4, buf[:n], oob[:oobn])
func ParseLinuxEE(oob []byte) (backend.PacketType, net.Addr, error) {
	scms, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return -1, nil, err
	}
	if len(scms) != 1 {
		return -1, nil, fmt.Errorf("expected exactly 1 control message (got %d)", len(scms))
	}
	if !isRecvErrMessage(scms) {
		return -1, nil, fmt.Errorf("unexpected control header: %#v", scms[0].Header)
	}

	var extErr unix.SockExtendedErr
	if _, err := binary.Decode(scms[0].Data, binary.NativeEndian, &extErr); err != nil {
		return -1, nil, err
	}

	pktType, err := packetType(extErr)
	if err != nil {
		return -1, nil, err
	}

	peer, err := soEEOffender(scms[0].Data)
	if err != nil {
		return -1, nil, err
	}

	return pktType, peer, nil
}

// Extracts a sockaddr of what generated the error. This should be part of
// x/sys/unix or syscall, but, sadly it isn't. The cleanest way of getting it is
// with SE_EE_OFFENDER() via cgo.
//
// The macro in linux/errqueue.h is:
//
//	#define SO_EE_OFFENDER(ee) ((struct sockaddr *) ((ee) + 1))
//
// Which means the address immediately follows struct sock_extended_err. Which
// means we need to be sure there's enough room in b for it. Which means that
// C.sizeof_struct_sockaddr_in (or _in6) should be added to the length of oob in
// readErr().
func soEEOffender(b []byte) (net.Addr, error) {
	sa := C.so_ee_offender((*C.struct_sock_extended_err)(unsafe.Pointer(&b[0])))
	host := [C.INET6_ADDRSTRLEN]C.char{} // More than enough for IPv4
	port := [5]C.char{}                  // Longest numeric port number
	if _, err := C.getnameinfo(sa, C.socklen_t(len(b)), &host[0], C.socklen_t(len(host)), &port[0], C.socklen_t(len(port)), C.NI_NUMERICHOST|C.NI_NUMERICSERV); err != nil {
		return nil, err
	}
	portNum, _ := strconv.Atoi(C.GoString(&port[0])) // Error means zero, which is fine.
	addr := net.UDPAddr{
		IP:   net.ParseIP(C.GoString(&host[0])),
		Port: portNum,
	}
	return &addr, nil
}

func isRecvErrMessage(scms []unix.SocketControlMessage) bool {
	h := scms[0].Header
	return (h.Type == unix.IP_RECVERR && h.Level == unix.IPPROTO_IP) ||
		(h.Type == unix.IPV6_RECVERR && h.Level == unix.IPPROTO_IPV6)
}

func packetType(extErr unix.SockExtendedErr) (backend.PacketType, error) {
	switch extErr.Origin {
	case unix.SO_EE_ORIGIN_ICMP:
		switch extErr.Type {
		case byte(ipv4.ICMPTypeTimeExceeded):
			return backend.PacketTimeExceeded, nil
		case byte(ipv4.ICMPTypeDestinationUnreachable):
			if extErr.Code == codePortUnreachableV4 {
				return backend.PacketReply, nil
			} else {
				return backend.PacketDestinationUnreachable, nil
			}
		}
	case unix.SO_EE_ORIGIN_ICMP6:
		switch extErr.Type {
		case byte(ipv6.ICMPTypeTimeExceeded):
			return backend.PacketTimeExceeded, nil
		case byte(ipv6.ICMPTypeDestinationUnreachable):
			if extErr.Code == codePortUnreachableV6 {
				return backend.PacketReply, nil
			} else {
				return backend.PacketDestinationUnreachable, nil
			}
		}
	default:
		return -1, fmt.Errorf("unrecognized origin %v", extErr.Origin)
	}
	return -1, fmt.Errorf("unrecognized packet type %v", extErr.Type)
}
