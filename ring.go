package main

// ring keeps the last len(buf) bytes written to it. Callers synchronize.
type ring struct {
	buf  []byte
	end  int // next write position
	full bool
}

func newRing(size int) ring { return ring{buf: make([]byte, size)} }

func (r *ring) Write(p []byte) {
	size := len(r.buf)
	if len(p) >= size {
		copy(r.buf, p[len(p)-size:])
		r.end, r.full = 0, true
		return
	}
	n := copy(r.buf[r.end:], p)
	copy(r.buf, p[n:])
	if r.end+len(p) >= size {
		r.full = true
	}
	r.end = (r.end + len(p)) % size
}

// Bytes returns a copy of the kept bytes, oldest first.
func (r *ring) Bytes() []byte {
	if !r.full {
		return append([]byte(nil), r.buf[:r.end]...)
	}
	out := make([]byte, 0, len(r.buf))
	out = append(out, r.buf[r.end:]...)
	return append(out, r.buf[:r.end]...)
}
