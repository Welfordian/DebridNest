package metrics

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	torrentmgr "github.com/debridnest/debridnest/internal/torrent"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	prometheusHandler := promhttp.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !prefersMetricsHTML(r) {
			prometheusHandler.ServeHTTP(w, r)
			return
		}

		body, status := capturePrometheusMetrics(prometheusHandler, r)
		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}

		page, err := renderMetricsPage(body, time.Now())
		if err != nil {
			http.Error(w, "failed to render metrics", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(page)
	})
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

type captureResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newCaptureResponseWriter() *captureResponseWriter {
	return &captureResponseWriter{
		header: make(http.Header),
		status: http.StatusOK,
	}
}

func (w *captureResponseWriter) Header() http.Header {
	return w.header
}

func (w *captureResponseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *captureResponseWriter) Write(p []byte) (int, error) {
	return w.body.Write(p)
}

func capturePrometheusMetrics(handler http.Handler, r *http.Request) (string, int) {
	req := r.Clone(r.Context())
	req.URL.RawQuery = ""
	req.Header = req.Header.Clone()
	req.Header.Set("Accept", "text/plain")
	req.Header.Del("Accept-Encoding")
	rec := newCaptureResponseWriter()
	handler.ServeHTTP(rec, req)
	return rec.body.String(), rec.status
}

func prefersMetricsHTML(r *http.Request) bool {
	query := r.URL.Query()
	raw := strings.ToLower(query.Get("raw"))
	format := strings.ToLower(query.Get("format"))
	if raw == "1" || raw == "true" || format == "text" || format == "prometheus" {
		return false
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

type metricsPageData struct {
	GeneratedAt  string
	GeneratedISO string
	FamilyCount  int
	SampleCount  int
	Highlights   []metricHighlight
	Families     []metricFamily
}

type metricHighlight struct {
	Label  string
	Value  string
	Detail string
	Tone   string
}

type metricFamily struct {
	Name    string
	Help    string
	Type    string
	Samples []metricSample
}

type metricSample struct {
	Name         string
	Labels       string
	Value        string
	DisplayValue string
}

func renderMetricsPage(metricsText string, generatedAt time.Time) ([]byte, error) {
	families := parseMetricFamilies(metricsText)
	data := metricsPageData{
		GeneratedAt:  generatedAt.Format("15:04:05 MST"),
		GeneratedISO: generatedAt.Format(time.RFC3339),
		FamilyCount:  len(families),
		SampleCount:  countMetricSamples(families),
		Highlights:   buildMetricHighlights(families),
		Families:     families,
	}

	var out bytes.Buffer
	if err := metricsPageTemplate.Execute(&out, data); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func parseMetricFamilies(metricsText string) []metricFamily {
	byName := make(map[string]*metricFamily)
	order := make([]string, 0)
	familyFor := func(name string) *metricFamily {
		if family, ok := byName[name]; ok {
			return family
		}
		family := &metricFamily{Name: name}
		byName[name] = family
		order = append(order, name)
		return family
	}

	for _, rawLine := range strings.Split(metricsText, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "# HELP "):
			name, help, ok := strings.Cut(strings.TrimPrefix(line, "# HELP "), " ")
			if ok && name != "" {
				familyFor(name).Help = help
			}
		case strings.HasPrefix(line, "# TYPE "):
			name, typ, ok := strings.Cut(strings.TrimPrefix(line, "# TYPE "), " ")
			if ok && name != "" {
				familyFor(name).Type = typ
			}
		case strings.HasPrefix(line, "#"):
			continue
		default:
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			sampleName, labels := splitMetricKey(fields[0])
			if sampleName == "" {
				continue
			}
			family := familyFor(sampleName)
			family.Samples = append(family.Samples, metricSample{
				Name:         sampleName,
				Labels:       labels,
				Value:        fields[1],
				DisplayValue: formatMetricValue(sampleName, fields[1]),
			})
		}
	}

	families := make([]metricFamily, 0, len(order))
	for _, name := range order {
		families = append(families, *byName[name])
	}
	sort.SliceStable(families, func(i, j int) bool {
		leftRank := metricFamilyRank(families[i].Name)
		rightRank := metricFamilyRank(families[j].Name)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return families[i].Name < families[j].Name
	})
	return families
}

func splitMetricKey(key string) (string, string) {
	name, rest, ok := strings.Cut(key, "{")
	if !ok {
		return key, ""
	}
	return name, strings.TrimSuffix(rest, "}")
}

func metricFamilyRank(name string) int {
	switch {
	case strings.HasPrefix(name, "debridnest_"):
		return 0
	case strings.HasPrefix(name, "process_"):
		return 1
	case strings.HasPrefix(name, "go_"):
		return 2
	default:
		return 3
	}
}

func countMetricSamples(families []metricFamily) int {
	total := 0
	for _, family := range families {
		total += len(family.Samples)
	}
	return total
}

func buildMetricHighlights(families []metricFamily) []metricHighlight {
	highlights := make([]metricHighlight, 0, 5)
	addValue := func(name, label, detail, tone string) {
		if value, ok := firstMetricValue(families, name); ok {
			highlights = append(highlights, metricHighlight{
				Label:  label,
				Value:  formatMetricValue(name, value),
				Detail: detail,
				Tone:   tone,
			})
		}
	}
	addSum := func(name, label, detail, tone string) {
		if value, ok := sumMetricValues(families, name); ok {
			highlights = append(highlights, metricHighlight{
				Label:  label,
				Value:  formatCompactNumber(value),
				Detail: detail,
				Tone:   tone,
			})
		}
	}

	addValue("debridnest_active_torrents", "Active torrents", "downloading or queued", "green")
	addValue("debridnest_disk_bytes_used", "Disk used", "local torrent files", "amber")
	addValue("debridnest_download_bytes_served_total", "Bytes served", "signed download traffic", "cyan")
	addSum("debridnest_http_requests_total", "HTTP requests", "recorded by the server", "pink")
	addValue("go_goroutines", "Goroutines", "Go runtime", "neutral")
	return highlights
}

func firstMetricValue(families []metricFamily, name string) (string, bool) {
	for _, family := range families {
		if family.Name == name && len(family.Samples) > 0 {
			return family.Samples[0].Value, true
		}
	}
	return "", false
}

func sumMetricValues(families []metricFamily, name string) (float64, bool) {
	for _, family := range families {
		if family.Name != name {
			continue
		}
		var total float64
		var found bool
		for _, sample := range family.Samples {
			value, err := strconv.ParseFloat(sample.Value, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				continue
			}
			total += value
			found = true
		}
		return total, found
	}
	return 0, false
}

func formatMetricValue(name string, raw string) string {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return raw
	}
	switch {
	case strings.Contains(name, "bytes"):
		return formatBytes(value)
	case strings.Contains(name, "seconds"):
		return formatSeconds(value)
	default:
		return formatCompactNumber(value)
	}
}

func formatBytes(value float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	abs := math.Abs(value)
	unit := 0
	for abs >= 1024 && unit < len(units)-1 {
		value /= 1024
		abs /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%.0f %s", value, units[unit])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

func formatSeconds(value float64) string {
	switch {
	case value < 0:
		return fmt.Sprintf("%.2f s", value)
	case value < 1:
		return fmt.Sprintf("%.0f ms", value*1000)
	case value < 60:
		return fmt.Sprintf("%.2f s", value)
	case value < 3600:
		return fmt.Sprintf("%.1f min", value/60)
	default:
		return fmt.Sprintf("%.1f h", value/3600)
	}
}

func formatCompactNumber(value float64) string {
	abs := math.Abs(value)
	switch {
	case abs >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", value/1_000_000_000)
	case abs >= 1_000_000:
		return fmt.Sprintf("%.1fM", value/1_000_000)
	case abs >= 1_000:
		return fmt.Sprintf("%.1fk", value/1_000)
	case math.Trunc(value) == value:
		return fmt.Sprintf("%.0f", value)
	default:
		return fmt.Sprintf("%.3g", value)
	}
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

var metricsPageTemplate = template.Must(template.New("metrics-page").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="dark">
  <title>DebridNest Metrics</title>
  <style>
    :root {
      color-scheme: dark;
      --bg: #050807;
      --surface: #0d1412;
      --surface-2: #111a17;
      --surface-3: #16211d;
      --line: #26342f;
      --line-strong: #3a4b44;
      --text: #eef6f2;
      --muted: #99aaa3;
      --faint: #65746e;
      --cyan: #34d7c3;
      --green: #64e69b;
      --amber: #f4bf5f;
      --pink: #ff7b93;
      --shadow: 0 24px 80px rgba(0, 0, 0, 0.34);
    }

    * { box-sizing: border-box; }

    html {
      min-height: 100%;
      background: var(--bg);
    }

    body {
      min-height: 100%;
      margin: 0;
      color: var(--text);
      background:
        linear-gradient(90deg, rgba(52, 215, 195, 0.04) 1px, transparent 1px),
        linear-gradient(0deg, rgba(244, 191, 95, 0.025) 1px, transparent 1px),
        var(--bg);
      background-size: 56px 56px;
      font: 15px/1.5 Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }

    a { color: inherit; }

    .shell {
      width: min(1440px, calc(100% - 48px));
      margin: 0 auto;
      padding: 32px 0 56px;
    }

    .topbar {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 24px;
      align-items: start;
      padding: 24px;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: rgba(13, 20, 18, 0.94);
      box-shadow: var(--shadow);
    }

    .identity {
      display: flex;
      gap: 16px;
      min-width: 0;
      align-items: center;
    }

    .mark {
      display: grid;
      width: 48px;
      height: 48px;
      flex: 0 0 auto;
      place-items: center;
      border: 1px solid rgba(52, 215, 195, 0.42);
      border-radius: 8px;
      color: #06100e;
      background: var(--cyan);
      font-weight: 850;
      letter-spacing: 0;
      box-shadow: 0 0 0 6px rgba(52, 215, 195, 0.08);
    }

    .eyebrow {
      margin: 0 0 4px;
      color: var(--cyan);
      font-size: 0.76rem;
      font-weight: 760;
      letter-spacing: 0.11em;
      text-transform: uppercase;
    }

    h1 {
      margin: 0;
      font-size: clamp(2rem, 4vw, 4.2rem);
      line-height: 0.95;
      letter-spacing: 0;
    }

    .meta {
      margin: 10px 0 0;
      color: var(--muted);
      font-size: 0.96rem;
    }

    .actions {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      justify-content: flex-end;
    }

    .button {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-height: 38px;
      padding: 0 14px;
      border: 1px solid var(--line-strong);
      border-radius: 8px;
      color: var(--text);
      background: var(--surface-2);
      font: inherit;
      font-weight: 720;
      text-decoration: none;
      cursor: pointer;
    }

    .button.primary {
      border-color: rgba(52, 215, 195, 0.48);
      color: #07100e;
      background: var(--cyan);
    }

    .button:focus-visible,
    .filter input:focus-visible {
      outline: 2px solid var(--amber);
      outline-offset: 2px;
    }

    .signal-strip {
      display: grid;
      grid-template-columns: repeat(18, 1fr);
      gap: 6px;
      margin-top: 18px;
      align-items: end;
      height: 34px;
    }

    .signal-strip span {
      display: block;
      min-width: 0;
      border-radius: 4px 4px 0 0;
      background: var(--line-strong);
    }

    .signal-strip span:nth-child(3n + 1) { height: 36%; background: rgba(52, 215, 195, 0.64); }
    .signal-strip span:nth-child(3n + 2) { height: 72%; background: rgba(244, 191, 95, 0.72); }
    .signal-strip span:nth-child(3n) { height: 52%; background: rgba(100, 230, 155, 0.58); }

    .summary-grid {
      display: grid;
      grid-template-columns: repeat(5, minmax(0, 1fr));
      gap: 12px;
      margin-top: 16px;
    }

    .metric-card {
      min-width: 0;
      padding: 18px;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: var(--surface);
    }

    .metric-card .label {
      display: flex;
      align-items: center;
      gap: 8px;
      color: var(--muted);
      font-size: 0.75rem;
      font-weight: 800;
      letter-spacing: 0.09em;
      text-transform: uppercase;
    }

    .metric-card .label::before {
      content: "";
      width: 8px;
      height: 8px;
      border-radius: 999px;
      background: var(--faint);
    }

    .metric-card.tone-green .label::before { background: var(--green); }
    .metric-card.tone-amber .label::before { background: var(--amber); }
    .metric-card.tone-cyan .label::before { background: var(--cyan); }
    .metric-card.tone-pink .label::before { background: var(--pink); }

    .metric-card strong {
      display: block;
      margin-top: 12px;
      font-size: clamp(1.7rem, 3vw, 2.55rem);
      line-height: 1;
      letter-spacing: 0;
      overflow-wrap: anywhere;
    }

    .metric-card .detail {
      display: block;
      margin-top: 10px;
      color: var(--muted);
      overflow-wrap: anywhere;
    }

    .panel {
      margin-top: 16px;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: rgba(13, 20, 18, 0.96);
      box-shadow: var(--shadow);
      overflow: hidden;
    }

    .panel-header {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 16px;
      align-items: center;
      padding: 18px 20px;
      border-bottom: 1px solid var(--line);
    }

    h2 {
      margin: 0;
      font-size: 1.15rem;
      letter-spacing: 0;
    }

    .panel-counts {
      color: var(--muted);
      font-size: 0.92rem;
      text-align: right;
    }

    .filter {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 12px;
      padding: 16px 20px;
      border-bottom: 1px solid var(--line);
      background: var(--surface-2);
    }

    .filter input {
      width: 100%;
      min-height: 42px;
      border: 1px solid var(--line-strong);
      border-radius: 8px;
      padding: 0 13px;
      color: var(--text);
      background: #08100e;
      font: inherit;
    }

    .filter input::placeholder { color: var(--faint); }

    .visible-count {
      align-self: center;
      color: var(--muted);
      font-weight: 700;
      white-space: nowrap;
    }

    .families {
      display: grid;
    }

    .family {
      display: grid;
      grid-template-columns: minmax(220px, 0.7fr) minmax(0, 1.3fr);
      gap: 16px;
      padding: 20px;
      border-bottom: 1px solid var(--line);
    }

    .family:last-child { border-bottom: 0; }

    .family-title {
      min-width: 0;
    }

    .family-title h3 {
      margin: 0;
      color: var(--text);
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
      font-size: 0.98rem;
      line-height: 1.35;
      overflow-wrap: anywhere;
    }

    .badges {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-top: 10px;
    }

    .badge {
      display: inline-flex;
      min-height: 25px;
      align-items: center;
      border: 1px solid var(--line-strong);
      border-radius: 999px;
      padding: 0 9px;
      color: var(--muted);
      background: var(--surface-3);
      font-size: 0.78rem;
      font-weight: 750;
      text-transform: uppercase;
      letter-spacing: 0.06em;
    }

    .family-help {
      margin: 12px 0 0;
      color: var(--muted);
      overflow-wrap: anywhere;
    }

    .sample-list {
      display: grid;
      gap: 8px;
      min-width: 0;
    }

    .sample {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 12px;
      align-items: center;
      min-width: 0;
      padding: 11px 12px;
      border: 1px solid rgba(38, 52, 47, 0.8);
      border-radius: 8px;
      background: #08100e;
    }

    .sample code {
      display: block;
      min-width: 0;
      color: #c8d7d1;
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
      font-size: 0.88rem;
      white-space: normal;
      overflow-wrap: anywhere;
    }

    .sample .labels {
      margin-top: 4px;
      color: var(--faint);
      font-size: 0.82rem;
    }

    .sample-value {
      min-width: 92px;
      color: var(--text);
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
      font-size: 1rem;
      text-align: right;
      overflow-wrap: anywhere;
    }

    .empty {
      padding: 28px 20px;
      color: var(--muted);
    }

    @media (max-width: 1100px) {
      .summary-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); }
      .family { grid-template-columns: 1fr; }
    }

    @media (max-width: 760px) {
      .shell {
        width: min(100% - 28px, 1440px);
        padding-top: 18px;
      }

      .topbar,
      .panel-header,
      .filter {
        grid-template-columns: 1fr;
      }

      .actions {
        justify-content: flex-start;
      }

      .summary-grid { grid-template-columns: 1fr; }
      .panel-counts { text-align: left; }
      .sample { grid-template-columns: 1fr; }
      .sample-value { text-align: left; }
      .signal-strip { grid-template-columns: repeat(9, 1fr); }
      .signal-strip span:nth-child(n + 10) { display: none; }
    }

    @media (prefers-reduced-motion: no-preference) {
      .button,
      .metric-card,
      .sample {
        transition: border-color 160ms ease, transform 160ms ease;
      }

      .button:hover,
      .metric-card:hover,
      .sample:hover {
        border-color: var(--line-strong);
      }

      .button:hover {
        transform: translateY(-1px);
      }
    }
  </style>
</head>
<body>
  <main class="shell">
    <section class="topbar" aria-labelledby="metrics-title">
      <div>
        <div class="identity">
          <span class="mark" aria-hidden="true">DN</span>
          <div>
            <p class="eyebrow">Prometheus scrape endpoint</p>
            <h1 id="metrics-title">DebridNest Metrics</h1>
            <p class="meta">Generated <time datetime="{{ .GeneratedISO }}">{{ .GeneratedAt }}</time></p>
          </div>
        </div>
        <div class="signal-strip" aria-hidden="true">
          <span></span><span></span><span></span><span></span><span></span><span></span>
          <span></span><span></span><span></span><span></span><span></span><span></span>
          <span></span><span></span><span></span><span></span><span></span><span></span>
        </div>
      </div>
      <div class="actions">
        <button class="button" type="button" onclick="window.location.reload()">Refresh</button>
        <a class="button" href="/dashboard/">Dashboard</a>
        <a class="button primary" href="/metrics?raw=1">Raw metrics</a>
      </div>
    </section>

    {{ if .Highlights }}
    <section class="summary-grid" aria-label="Metric highlights">
      {{ range .Highlights }}
      <article class="metric-card tone-{{ .Tone }}">
        <span class="label">{{ .Label }}</span>
        <strong>{{ .Value }}</strong>
        <span class="detail">{{ .Detail }}</span>
      </article>
      {{ end }}
    </section>
    {{ end }}

    <section class="panel" aria-labelledby="families-title">
      <div class="panel-header">
        <h2 id="families-title">Metric families</h2>
        <span class="panel-counts">{{ .FamilyCount }} families / {{ .SampleCount }} samples</span>
      </div>
      <label class="filter">
        <input id="metric-filter" type="search" autocomplete="off" spellcheck="false" placeholder="Filter metrics by name, label, or help text">
        <span id="visible-count" class="visible-count">{{ .FamilyCount }} visible</span>
      </label>
      <div id="families" class="families">
        {{ range .Families }}
        <article class="family" data-search="{{ .Name }} {{ .Help }} {{ range .Samples }}{{ .Labels }} {{ end }}">
          <div class="family-title">
            <h3>{{ .Name }}</h3>
            <div class="badges">
              {{ if .Type }}<span class="badge">{{ .Type }}</span>{{ end }}
              <span class="badge">{{ len .Samples }} samples</span>
            </div>
            {{ if .Help }}<p class="family-help">{{ .Help }}</p>{{ end }}
          </div>
          <div class="sample-list">
            {{ range .Samples }}
            <div class="sample">
              <div>
                <code>{{ .Name }}</code>
                {{ if .Labels }}<div class="labels">{{ .Labels }}</div>{{ end }}
              </div>
              <strong class="sample-value" title="{{ .Value }}">{{ .DisplayValue }}</strong>
            </div>
            {{ else }}
            <p class="empty">No samples reported for this family.</p>
            {{ end }}
          </div>
        </article>
        {{ end }}
      </div>
    </section>
  </main>
  <script>
    (() => {
      const input = document.getElementById("metric-filter");
      const families = Array.from(document.querySelectorAll(".family"));
      const count = document.getElementById("visible-count");
      const update = () => {
        const query = input.value.trim().toLowerCase();
        let visible = 0;
        for (const family of families) {
          const match = !query || family.dataset.search.toLowerCase().includes(query);
          family.hidden = !match;
          if (match) visible += 1;
        }
        count.textContent = visible + " visible";
      };
      input.addEventListener("input", update);
    })();
  </script>
</body>
</html>
`))
