// Package udp implements UDP pings.
package udp

import (
	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/util"
)

const (
	maxMTU = 1500

	udpProtoNum = 17

	icmpV4CodePortUnreachable = 3
	icmpV6CodePortUnreachable = 4

	ipv6FragmentType   = 44
	ipv6FragmentExtLen = 8

	// https://www.iana.org/assignments/service-names-port-numbers/service-names-port-numbers.xhtml?search=33434
	defaultBasePort = 33434
)

func init() {
	backend.Register("udp", func(ipVer util.IPVersion) (backend.Conn, error) { return New(ipVer) })
}
