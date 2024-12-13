//go:build !rawsock && (darwin || linux)

package privsep

import (
	"fmt"
	"os"
	"runtime"
)

func usePrivsep() bool {
	if os.Getuid() != os.Geteuid() {
		fmt.Fprintf(os.Stderr, `Error: running with setuid.

This is unnecessary and unsafe on %s. Please remove the setuid bit
using something like:

    sudo chmod u-s %s
`, runtime.GOOS, os.Args[0])
		os.Exit(1)
	}

	return false
}
