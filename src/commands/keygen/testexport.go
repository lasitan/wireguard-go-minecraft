package keygen

func ClampKey(key *[32]byte) {
	clampKey(key)
}

func DecodeKey32(b64 string) ([32]byte, error) {
	return decodeKey32(b64)
}
