package applog

import (
	"fmt"
	"os"
	"sync"
)

// rotator is an append-only file that starts over at max bytes, keeping
// the previous keep parts as path.1 (newest) … path.<keep>.
type rotator struct {
	mu      sync.Mutex
	path    string
	max     int64
	keep    int
	f       *os.File // nil once a write failed: logging is off, punk0 goes on
	size    int64
	retryAt int64 // size at which a rotation that failed is tried again
}

func openRotator(path string, max int64, keep int) (*rotator, error) {
	r := &rotator{path: path, max: max, keep: keep}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotator) open() error {
	f, err := os.OpenFile(r.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, st.Size()
	return nil
}

// Write scrubs p (Redact) and appends it. It never returns an error: a log
// that cannot be written is turned off rather than failing what was logged.
func (r *rotator) Write(p []byte) (int, error) {
	n := len(p)
	p = Redact(p)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return n, nil
	}
	if r.size > 0 && r.size+int64(len(p)) > r.max && r.size >= r.retryAt {
		r.rotate()
		if r.f == nil {
			return n, nil
		}
	}
	m, err := r.f.Write(p)
	r.size += int64(m)
	if err != nil {
		r.f.Close()
		r.f = nil
	}
	return n, nil
}

// rotate closes the file first: Windows cannot rename a file this process
// holds open. A hook process appending at that moment can still block the
// rename there; the file then grows a little and the rotation is tried again.
func (r *rotator) rotate() {
	r.f.Close()
	r.f = nil
	for i := r.keep - 1; i >= 1; i-- {
		os.Rename(fmt.Sprintf("%s.%d", r.path, i), fmt.Sprintf("%s.%d", r.path, i+1))
	}
	renamed := os.Rename(r.path, r.path+".1") == nil
	if r.open() != nil {
		return
	}
	r.retryAt = 0
	if !renamed {
		r.retryAt = r.size + r.max/8
	}
}
