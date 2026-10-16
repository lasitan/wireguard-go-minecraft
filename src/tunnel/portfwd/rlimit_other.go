//go:build !unix

package portfwd

func raiseNOFILE(int) {}
