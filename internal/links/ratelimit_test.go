package links

import (
	"bytes"
	"io"
	"testing"
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
