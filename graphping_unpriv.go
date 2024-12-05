//go:build !rawsock && darwin

package main

import (
	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/backend/icmp"
	"github.com/pcekm/graphping/internal/util"
)

func newV4Conn() (backend.Conn, error) {
	return icmp.New(util.IPv4)
}

func newV6Conn() (backend.Conn, error) {
	return icmp.New(util.IPv6)
}
