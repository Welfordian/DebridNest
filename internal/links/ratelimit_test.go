package links

import (
	"bytes"
	"io"
	"testing"
	"time"
)

func TestRateLimiterReader(t *testing.T) {
	data := make([]byte, 4096)
	src := bytes.NewReader(data)
	rl := NewRateLimiter(1)
	limited := rl.Reader(src)
	out, err := io.ReadAll(limited)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(out) != len(data) {
		t.Fatalf("read %d bytes, want %d", len(out), len(data))
	}
}

type fakeLimiterClock struct {
	t      time.Time
	sleeps []time.Duration
}

func (c *fakeLimiterClock) now() time.Time {
	return c.t
}

func (c *fakeLimiterClock) sleep(d time.Duration) {
	c.sleeps = append(c.sleeps, d)
	c.t = c.t.Add(d)
}

func TestRateLimiterReaderWaitsWhenAllowanceExhausted(t *testing.T) {
	clock := &fakeLimiterClock{t: time.Unix(123, 0)}
	rl := &RateLimiter{
		bytesPerSec: 100,
		last:        clock.t,
		now:         clock.now,
		sleep:       clock.sleep,
	}
	limited := rl.Reader(bytes.NewReader([]byte("hello")))

	buf := make([]byte, 5)
	n, err := limited.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != len(buf) {
		t.Fatalf("read %d bytes, want %d", n, len(buf))
	}
	if string(buf) != "hello" {
		t.Fatalf("got %q", buf)
	}
	if len(clock.sleeps) != 1 {
		t.Fatalf("slept %d times, want 1", len(clock.sleeps))
	}
	if got, want := clock.sleeps[0], 50*time.Millisecond; got != want {
		t.Fatalf("sleep = %v, want %v", got, want)
	}
}

type testReadSeekCloser struct {
	*bytes.Reader
}

func (testReadSeekCloser) Close() error { return nil }

func TestRateLimiterReadSeekCloser(t *testing.T) {
	data := []byte("hello world")
	src := testReadSeekCloser{bytes.NewReader(data)}
	rl := NewRateLimiter(0)
	rsc := rl.ReadSeekCloser(src)
	out, err := io.ReadAll(rsc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(out) != "hello world" {
		t.Fatalf("got %q", out)
	}
	if _, err := rsc.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek: %v", err)
	}
}
