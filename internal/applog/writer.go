package applog

import (
	"bytes"
	"io"
	"sync"
)

const defaultCapacity = 500

var defaultBuffer = New(defaultCapacity)

type Buffer struct {
	mu       sync.RWMutex
	lines    []string
	capacity int
}

func New(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = defaultCapacity
	}
	return &Buffer{capacity: capacity}
}

type teeWriter struct {
	dst    io.Writer
	buffer *Buffer
}

func NewWriter(dst io.Writer) io.Writer {
	return &teeWriter{dst: dst, buffer: defaultBuffer}
}

func (t *teeWriter) Write(p []byte) (int, error) {
	n, err := t.dst.Write(p)
	if n > 0 {
		t.buffer.append(p[:n])
	}
	return n, err
}

func (b *Buffer) append(p []byte) {
	for _, line := range bytes.Split(p, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		b.mu.Lock()
		b.lines = append(b.lines, string(line))
		if len(b.lines) > b.capacity {
			b.lines = b.lines[len(b.lines)-b.capacity:]
		}
		b.mu.Unlock()
	}
}

func Recent(limit int) []string {
	return defaultBuffer.Recent(limit)
}

func (b *Buffer) Recent(limit int) []string {
	if limit <= 0 {
		limit = 200
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	n := len(b.lines)
	if n == 0 {
		return nil
	}
	start := 0
	if n > limit {
		start = n - limit
	}
	out := make([]string, n-start)
	copy(out, b.lines[start:])
	return out
}
