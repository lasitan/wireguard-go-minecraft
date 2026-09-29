package tun

import "unsafe"

type RateJuggler = rateJuggler

func NativeTunRateFieldOffset() uintptr {
	var n NativeTun
	return unsafe.Offsetof(n.rate)
}

func RateJugglerCurrentOffset() uintptr {
	var r rateJuggler
	return unsafe.Offsetof(r.current)
}

func RateJugglerNextByteCountOffset() uintptr {
	var r rateJuggler
	return unsafe.Offsetof(r.nextByteCount)
}

func RateJugglerNextStartTimeOffset() uintptr {
	var r rateJuggler
	return unsafe.Offsetof(r.nextStartTime)
}
