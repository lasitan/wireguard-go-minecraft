package relay

import "testing"

func TestHairpinLoopsWritesBackToRead(t *testing.T) {
	h := newHairpinTUN()
	defer h.Close()
	if ev := <-h.Events(); ev == 0 {
		t.Fatal("device must see the TUN as up")
	}
	const off = 16
	in := [][]byte{append(make([]byte, off), 1, 2, 3), append(make([]byte, off), 4, 5)}
	if n, err := h.Write(in, off); err != nil || n != 2 {
		t.Fatalf("write: %d %v", n, err)
	}
	bufs := [][]byte{make([]byte, 64), make([]byte, 64), make([]byte, 64)}
	sizes := make([]int, 3)
	n, err := h.Read(bufs, sizes, off)
	if err != nil || n != 2 || sizes[0] != 3 || sizes[1] != 2 || bufs[1][off] != 4 {
		t.Fatalf("read: n=%d sizes=%v err=%v", n, sizes, err)
	}
	h.Close()
	if _, err := h.Read(bufs, sizes, off); err == nil {
		t.Fatal("read after close must fail")
	}
}
