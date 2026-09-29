package ratelimiter

import "time"

const (
	PacketsPerSecond = packetsPerSecond
	PacketsBurstable = packetsBurstable
)

func (rate *Ratelimiter) SetTimeNow(f func() time.Time) {
	rate.timeNow = f
}

func (rate *Ratelimiter) TestLock() {
	rate.mu.Lock()
}

func (rate *Ratelimiter) TestUnlock() {
	rate.mu.Unlock()
}

func (rate *Ratelimiter) TestCleanup() {
	rate.cleanup()
}
