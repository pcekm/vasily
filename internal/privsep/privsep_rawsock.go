//go:build rawsock || !darwin

package privsep

// Stub out the check since this platform either always runs with privsep, or it
// was explicitly compiled to use it.
func checkSetuid() {}
