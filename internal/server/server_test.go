package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestDashboardHandlerFallsBackToIndexForRoutes(t *testing.T) {
	dashboard := fstest.MapFS{
		"index.html":        &fstest.MapFile{Data: []byte("<html>dashboard</html>")},
		"assets/app.js":     &fstest.MapFile{Data: []byte("console.log('dashboard')")},
		"assets/style.css":  &fstest.MapFile{Data: []byte("body{}")},
		"assets/image.webp": &fstest.MapFile{Data: []byte("image")},
	}
	handler := dashboardHandler(fs.FS(dashboard))

	tests := []struct {
		path       string
		wantCode   int
		wantBody   string
		wantPrefix string
	}{
		{path: "/dashboard/", wantCode: http.StatusOK, wantBody: "dashboard"},
		{path: "/dashboard/torrents", wantCode: http.StatusOK, wantBody: "dashboard"},
		{path: "/dashboard/library/details", wantCode: http.StatusOK, wantBody: "dashboard"},
		{path: "/dashboard/assets/app.js", wantCode: http.StatusOK, wantBody: "console.log"},
		{path: "/dashboard/assets/missing.js", wantCode: http.StatusNotFound, wantPrefix: "404"},
		{path: "/dashboard/missing.png", wantCode: http.StatusNotFound, wantPrefix: "404"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d; body=%q", rec.Code, tc.wantCode, rec.Body.String())
			}
			body := rec.Body.String()
			if tc.wantBody != "" && !strings.Contains(body, tc.wantBody) {
				t.Fatalf("body = %q, want contains %q", body, tc.wantBody)
			}
			if tc.wantPrefix != "" && !strings.HasPrefix(body, tc.wantPrefix) {
				t.Fatalf("body = %q, want prefix %q", body, tc.wantPrefix)
			}
		})
	}
}
