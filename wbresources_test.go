package main

import (
	"testing"
	"time"
)

func TestCPUMeter(t *testing.T) {
	var m cpuMeter
	t0 := time.Unix(1000, 0)
	if p := m.sample(t0, map[string]time.Duration{"a": time.Second}); p["a"] != 0 {
		t.Fatalf("first sample: %v", p)
	}
	// Half a core over two seconds.
	if p := m.sample(t0.Add(2*time.Second), map[string]time.Duration{"a": 2 * time.Second}); p["a"] != 50 {
		t.Fatalf("after 2s: %v", p)
	}
	// Too soon: the last figure stands, and the next interval runs from t0+2s.
	if p := m.sample(t0.Add(2100*time.Millisecond), map[string]time.Duration{"a": 5 * time.Second}); p["a"] != 50 {
		t.Fatalf("too soon: %v", p)
	}
	if p := m.sample(t0.Add(3*time.Second), map[string]time.Duration{"a": 3 * time.Second}); p["a"] != 100 {
		t.Fatalf("after 3s: %v", p)
	}
	// A child exited, so the tree's time went down: zero, not negative.
	if p := m.sample(t0.Add(4*time.Second), map[string]time.Duration{"a": time.Second}); p["a"] != 0 {
		t.Fatalf("shrunk: %v", p)
	}
	// A row that disappears starts over when it comes back.
	m.sample(t0.Add(5*time.Second), map[string]time.Duration{})
	if p := m.sample(t0.Add(6*time.Second), map[string]time.Duration{"a": time.Hour}); p["a"] != 0 {
		t.Fatalf("returning row: %v", p)
	}
}
