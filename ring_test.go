package main

import (
	"bytes"
	"testing"
)

func TestRingKeepsTail(t *testing.T) {
	// Every write size, including ones that land exactly on the boundary and
	// ones larger than the ring, must leave the tail of everything written.
	for size := 1; size <= 9; size++ {
		for step := 1; step <= 12; step++ {
			r := newRing(size)
			var all []byte
			for i := 0; i < 5; i++ {
				p := bytes.Repeat([]byte{byte('a' + i)}, step)
				p[0] = byte('0' + i)
				r.Write(p)
				all = append(all, p...)
				want := all
				if len(want) > size {
					want = want[len(want)-size:]
				}
				if got := r.Bytes(); !bytes.Equal(got, want) {
					t.Fatalf("size %d step %d write %d: got %q, want %q", size, step, i, got, want)
				}
			}
		}
	}
}

func TestRingEmpty(t *testing.T) {
	r := newRing(4)
	if got := r.Bytes(); len(got) != 0 {
		t.Fatalf("got %q, want empty", got)
	}
}
