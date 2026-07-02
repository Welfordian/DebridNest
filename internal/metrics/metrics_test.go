package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesHTMLForBrowserAccept(t *testing.T) {
	handler := (&Collector{}).Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("content type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"<!doctype html>", "DebridNest Metrics", "Metric families", "/metrics?raw=1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("HTML body missing %q", want)
		}
	}
	if strings.Contains(body, "���") {
		t.Fatal("HTML body appears to contain compressed metrics bytes")
	}
}

func TestHandlerKeepsPlainTextForScrapers(t *testing.T) {
	handler := (&Collector{}).Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Accept", "*/*")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("content type = %q, want text/plain", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "# HELP") {
		t.Fatal("plain metrics body missing HELP lines")
	}
	if strings.Contains(body, "<!doctype html>") {
		t.Fatal("plain metrics response unexpectedly contained HTML")
	}
}

func TestHandlerRawQueryOverridesBrowserAccept(t *testing.T) {
	handler := (&Collector{}).Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics?raw=1", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("content type = %q, want text/plain", ct)
	}
	if strings.Contains(rec.Body.String(), "<!doctype html>") {
		t.Fatal("raw metrics response unexpectedly contained HTML")
	}
}
