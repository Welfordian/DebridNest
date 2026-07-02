package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/debridnest/debridnest/internal/activity"
	"github.com/debridnest/debridnest/internal/api/admin"
	"github.com/debridnest/debridnest/internal/api/qbit"
	"github.com/debridnest/debridnest/internal/api/rd"
	"github.com/debridnest/debridnest/internal/auth"
	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/metrics"
	"github.com/debridnest/debridnest/internal/retention"
	"github.com/debridnest/debridnest/internal/settings"
	"github.com/debridnest/debridnest/internal/torrent"
	"github.com/debridnest/debridnest/internal/transcode"
	"github.com/debridnest/debridnest/internal/web"
	"github.com/debridnest/debridnest/internal/webdav"
)

type Options struct {
	Config          config.Config
	Manager         *torrent.Manager
	Signer          *links.Signer
	Metrics         *metrics.Collector
	RetentionRunner *retention.Runner
	Auth            *auth.Service
	Settings        *settings.Store
	Activity        *activity.Service
}

func NewRouter(opts Options) (chi.Router, error) {
	rdHandler := rd.NewHandler(opts.Config, opts.Manager, opts.Signer, opts.Metrics, opts.Auth)
	rdRoutes := rdHandler.Routes()
	adminHandler := admin.NewHandler(opts.Config, opts.Manager, opts.RetentionRunner, opts.Activity, opts.Settings, opts.Auth)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	if opts.Metrics != nil {
		r.Use(opts.Metrics.Middleware)
	}
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	if opts.Metrics != nil {
		r.Handle("/metrics", opts.Metrics.Handler())
	}
	r.Mount("/rest/1.0", rdRoutes)
	r.Mount("/api/v1", adminHandler.Routes())
	qbit.Mount(r, opts.Config, opts.Manager, opts.Auth)
	r.Get("/d/{linkID}", func(w http.ResponseWriter, req *http.Request) {
		req.URL.Path = "/d/" + chi.URLParam(req, "linkID")
		rdRoutes.ServeHTTP(w, req)
	})
	r.Handle("/dl/*", http.HandlerFunc(rdHandler.ServeDownloadPublic))

	if err := webdav.Mount(r, opts.Config, opts.Manager); err != nil {
		return nil, err
	}
	if err := transcode.Mount(r, opts.Config, opts.Manager, opts.Signer); err != nil {
		return nil, err
	}

	dashboard, err := fs.Sub(web.Dashboard, "dashboard")
	if err != nil {
		return nil, err
	}
	r.Handle("/dashboard/*", dashboardHandler(dashboard))
	r.Get("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard/", http.StatusFound)
	})

	return r, nil
}

func dashboardHandler(dashboard fs.FS) http.Handler {
	fileServer := http.StripPrefix("/dashboard/", http.FileServer(http.FS(dashboard)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/dashboard/")
		clean := strings.TrimPrefix(path.Clean("/"+name), "/")
		if clean == "." || clean == "" {
			serveDashboardIndex(w, r, dashboard)
			return
		}

		if info, err := fs.Stat(dashboard, clean); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		if strings.HasPrefix(clean, "assets/") || path.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}

		serveDashboardIndex(w, r, dashboard)
	})
}

func serveDashboardIndex(w http.ResponseWriter, r *http.Request, dashboard fs.FS) {
	data, err := fs.ReadFile(dashboard, "index.html")
	if err != nil {
		http.Error(w, "dashboard not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
