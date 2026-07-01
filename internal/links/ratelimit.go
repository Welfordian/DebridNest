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
	now         func() time.Time
	sleep       func(time.Duration)
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

type readSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

func (r *RateLimiter) ReadSeekCloser(src readSeekCloser) readSeekCloser {
	if r == nil {
		return src
	}
	return &rateLimitedReadSeekCloser{r: r, src: src}
}

type rateLimitedReadSeekCloser struct {
	r   *RateLimiter
	src readSeekCloser
}

func (l *rateLimitedReadSeekCloser) Read(p []byte) (int, error) {
	return l.r.Reader(l.src).Read(p)
}

func (l *rateLimitedReadSeekCloser) Seek(offset int64, whence int) (int64, error) {
	return l.src.Seek(offset, whence)
}

func (l *rateLimitedReadSeekCloser) Close() error {
	return l.src.Close()
}

type rateLimitedReader struct {
	r   *RateLimiter
	src io.Reader
}

func (l *rateLimitedReader) Read(p []byte) (int, error) {
	if l.r.bytesPerSec <= 0 || len(p) == 0 {
		return l.src.Read(p)
	}

	max := l.r.take(len(p))

	n, err := l.src.Read(p[:max])
	if n < max {
		l.r.refund(max - n)
	}
	return n, err
}

func (r *RateLimiter) take(max int) int {
	for {
		r.mu.Lock()
		now := r.currentTime()
		r.refillLocked(now)

		want := max
		if burst := r.burstSize(); want > burst {
			want = burst
		}
		if r.allowance >= float64(want) {
			r.allowance -= float64(want)
			r.mu.Unlock()
			return want
		}

		wait := r.waitDuration(float64(want) - r.allowance)
		r.mu.Unlock()
		r.sleepFor(wait)
	}
}

func (r *RateLimiter) refund(n int) {
	if n <= 0 {
		return
	}

	r.mu.Lock()
	r.refillLocked(r.currentTime())
	r.allowance += float64(n)
	if burst := r.burstCapacity(); r.allowance > burst {
		r.allowance = burst
	}
	r.mu.Unlock()
}

func (r *RateLimiter) refillLocked(now time.Time) {
	if r.last.IsZero() {
		r.last = now
		r.allowance = r.burstCapacity()
		return
	}

	elapsed := now.Sub(r.last).Seconds()
	if elapsed <= 0 {
		return
	}

	r.allowance += elapsed * r.bytesPerSec
	if burst := r.burstCapacity(); r.allowance > burst {
		r.allowance = burst
	}
	r.last = now
}

func (r *RateLimiter) burstSize() int {
	return int(r.burstCapacity())
}

func (r *RateLimiter) burstCapacity() float64 {
	if r.bytesPerSec < 1 {
		return 1
	}
	return r.bytesPerSec
}

func (r *RateLimiter) waitDuration(missing float64) time.Duration {
	wait := time.Duration(missing / r.bytesPerSec * float64(time.Second))
	if wait <= 0 {
		return time.Nanosecond
	}
	return wait
}

func (r *RateLimiter) currentTime() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

func (r *RateLimiter) sleepFor(d time.Duration) {
	if r.sleep != nil {
		r.sleep(d)
		return
	}
	time.Sleep(d)
}
