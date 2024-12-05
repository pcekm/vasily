//go:build rawsock || !darwin

package main

import (
	"github.com/pcekm/graphping/internal/backend"
	"github.com/pcekm/graphping/internal/privsep"
	"github.com/pcekm/graphping/internal/util"
)

func newV4Conn() (backend.Conn, error) {
	return privsep.Client.NewConn(util.IPv4)
}

func newV6Conn() (backend.Conn, error) {
	return privsep.Client.NewConn(util.IPv4)
}
