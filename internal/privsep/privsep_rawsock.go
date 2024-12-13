//go:build rawsock || !(darwin || linux)

package privsep

func usePrivsep() bool {
	return true
}
