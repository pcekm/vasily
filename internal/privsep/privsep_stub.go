//go:build !rawsock && darwin

package privsep

import (
	"fmt"
	"os"

	"github.com/pcekm/graphping/internal/privsep/client"
)

var (
	Client *client.Client
)

func Initialize() func() {

	if os.Getuid() != os.Geteuid() {
		fmt.Fprintf(os.Stderr, `Error: running with setuid.

This is unnecessary and unsafe on MacOS. Please remove the setuid bit
using something like:

    sudo chmod u-s %s
`, os.Args[0])
		os.Exit(1)
	}

	return func() {}
}
