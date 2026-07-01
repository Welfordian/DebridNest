package applog

import (
	"bytes"
	"strings"
	"testing"
)

func TestRecentRingBuffer(t *testing.T) {
	buf := New(3)
	w := &teeWriter{dst: &bytes.Buffer{}, buffer: buf}
	for _, line := range []string{"one", "two", "three", "four"} {
		_, _ = w.Write([]byte(line + "\n"))
	}

	got := buf.Recent(10)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if strings.Join(got, ",") != "two,three,four" {
		t.Fatalf("got %v", got)
	}
}

func TestNewWriterTeesToDst(t *testing.T) {
	var dst bytes.Buffer
	w := NewWriter(&dst)
	_, _ = w.Write([]byte("hello\n"))
	if dst.String() != "hello\n" {
		t.Fatalf("dst = %q", dst.String())
	}
	got := Recent(1)
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("recent = %v", got)
	}
}
