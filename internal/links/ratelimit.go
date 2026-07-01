package links

import (
	"io"
	"sync"
	"time"
)

type RateLimiter struct {
	bytesPerSec float64
	mu          sync.Mutex
	last        time.Time
	allowance   float64
}

func NewRateLimiter(mbps float64) *RateLimiter {
	if mbps <= 0 {
		return nil
	}
	return &RateLimiter{bytesPerSec: mbps * 1024 * 1024}
}

func (r *RateLimiter) Reader(src io.Reader) io.Reader {
	if r == nil {
		return src
	}
	return &rateLimitedReader{r: r, src: src}
}

type rateLimitedReader struct {
	r   *RateLimiter
	src io.Reader
}

func (l *rateLimitedReader) Read(p []byte) (int, error) {
	if l.r.bytesPerSec <= 0 {
		return l.src.Read(p)
	}

	l.r.mu.Lock()
	now := time.Now()
	if l.r.last.IsZero() {
		l.r.last = now
		l.r.allowance = l.r.bytesPerSec
	} else {
		elapsed := now.Sub(l.r.last).Seconds()
		l.r.allowance += elapsed * l.r.bytesPerSec
		if l.r.allowance > l.r.bytesPerSec {
			l.r.allowance = l.r.bytesPerSec
		}
		l.r.last = now
	}
	max := int(l.r.allowance)
	if max > len(p) {
		max = len(p)
	}
	if max <= 0 {
		max = 1
	}
	l.r.mu.Unlock()

	n, err := l.src.Read(p[:max])
	if n > 0 {
		l.r.mu.Lock()
		l.r.allowance -= float64(n)
		l.r.mu.Unlock()
	}
	return n, err
}
