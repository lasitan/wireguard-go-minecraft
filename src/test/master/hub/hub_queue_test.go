package hub_test

import (
	"testing"

	. "golang.zx2c4.com/wireguard/src/master/hub"
)

func TestQueueConfigNeverGoesBackwards(t *testing.T) {
	if got := QueuedRevisionAfter(5, 7, 6); got != 7 {
		t.Fatalf("pending revision %d, want 7", got)
	}
	if got := QueuedRevisionAfter(5, 5); got != 5 {
		t.Fatalf("pending revision %d, want 5", got)
	}
	if got := QueuedRevisionAfter(3, 4); got != 4 {
		t.Fatalf("pending revision %d, want 4", got)
	}
}
