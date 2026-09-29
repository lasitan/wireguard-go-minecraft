package tun

func Checksum(b []byte, initial uint64) uint16 {
	return checksum(b, initial)
}

func PseudoHeaderChecksumNoFold(protocol uint8, srcAddr, dstAddr []byte, totalLen uint16) uint64 {
	return pseudoHeaderChecksumNoFold(protocol, srcAddr, dstAddr, totalLen)
}
