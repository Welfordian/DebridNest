package metrics

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	torrentmgr "github.com/debridnest/debridnest/internal/torrent"
)

type Collector struct {
	requests       *prometheus.CounterVec
	activeTorrents prometheus.Gauge
	diskBytes      prometheus.Gauge
	downloadBytes  prometheus.Counter
}

func New() *Collector {
	return &Collector{
		requests: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "debridnest_http_requests_total",
			Help: "Total HTTP requests by method, path, and status code.",
		}, []string{"method", "path", "code"}),
		activeTorrents: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "debridnest_active_torrents",
			Help: "Number of torrents currently downloading or queued.",
		}),
		diskBytes: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "debridnest_disk_bytes_used",
			Help: "Bytes used on disk under the torrent files directory.",
		}),
		downloadBytes: promauto.NewCounter(prometheus.CounterOpts{
			Name: "debridnest_download_bytes_served_total",
			Help: "Total bytes served on signed download URLs.",
		}),
	}
}

func (c *Collector) Handler() http.Handler {
	return promhttp.Handler()
}

func (c *Collector) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		c.requests.WithLabelValues(r.Method, normalizePath(r.URL.Path), strconv.Itoa(rec.status)).Inc()
	})
}

func (c *Collector) RecordDownloadBytes(n int64) {
	if n > 0 {
		c.downloadBytes.Add(float64(n))
	}
}

func (c *Collector) StartStatsCollector(ctx context.Context, manager *torrentmgr.Manager, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stats, err := manager.Stats(ctx)
				if err != nil {
					continue
				}
				c.activeTorrents.Set(float64(stats.ActiveCount))
				c.diskBytes.Set(float64(stats.DiskUsed))
			}
		}
	}()
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

type ByteCountWriter struct {
	http.ResponseWriter
	n atomic.Int64
}

func (w *ByteCountWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if n > 0 {
		w.n.Add(int64(n))
	}
	return n, err
}

func (w *ByteCountWriter) Bytes() int64 {
	return w.n.Load()
}

func WrapDownloadWriter(w http.ResponseWriter) (*ByteCountWriter, http.ResponseWriter) {
	bcw := &ByteCountWriter{ResponseWriter: w}
	return bcw, bcw
}

func normalizePath(path string) string {
	switch {
	case path == "/healthz", path == "/metrics", path == "/dashboard", strings.HasPrefix(path, "/dashboard/"):
		return path
	case strings.HasPrefix(path, "/webdav/"):
		return "/webdav/*"
	case strings.HasPrefix(path, "/rest/1.0/dl/"):
		return "/rest/1.0/dl/*"
	case strings.HasPrefix(path, "/dl/"):
		return "/dl/*"
	case strings.HasPrefix(path, "/d/"):
		return "/d/{linkID}"
	case strings.HasPrefix(path, "/rest/1.0/torrents/info/"):
		return "/rest/1.0/torrents/info/{id}"
	case strings.HasPrefix(path, "/rest/1.0/torrents/selectFiles/"):
		return "/rest/1.0/torrents/selectFiles/{id}"
	case strings.HasPrefix(path, "/rest/1.0/torrents/delete/"):
		return "/rest/1.0/torrents/delete/{id}"
	case strings.HasPrefix(path, "/rest/1.0/torrents/instantAvailability/"):
		return "/rest/1.0/torrents/instantAvailability/*"
	case strings.HasPrefix(path, "/api/v1/torrents/") && strings.HasSuffix(path, "/retry"):
		return "/api/v1/torrents/{id}/retry"
	case strings.HasPrefix(path, "/api/v1/torrents/"):
		return "/api/v1/torrents/{id}"
	default:
		return path
	}
}
