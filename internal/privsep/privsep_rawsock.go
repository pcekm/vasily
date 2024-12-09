//go:build rawsock || !darwin

package privsep

func usePrivsep() bool {
	return true
}
