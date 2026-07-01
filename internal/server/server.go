package server

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/debridnest/debridnest/internal/api/admin"
	"github.com/debridnest/debridnest/internal/api/rd"
	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/metrics"
	"github.com/debridnest/debridnest/internal/torrent"
	"github.com/debridnest/debridnest/internal/web"
	"github.com/debridnest/debridnest/internal/webdav"
)

type Options struct {
	Config  config.Config
	Manager *torrent.Manager
	Signer  *links.Signer
	Metrics *metrics.Collector
}

func NewRouter(opts Options) (chi.Router, error) {
	rdHandler := rd.NewHandler(opts.Config, opts.Manager, opts.Signer, opts.Metrics)
	rdRoutes := rdHandler.Routes()
	adminHandler := admin.NewHandler(opts.Config, opts.Manager)

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
	r.Get("/d/{linkID}", func(w http.ResponseWriter, req *http.Request) {
		req.URL.Path = "/d/" + chi.URLParam(req, "linkID")
		rdRoutes.ServeHTTP(w, req)
	})
	r.Handle("/dl/*", http.HandlerFunc(rdHandler.ServeDownloadPublic))

	if err := webdav.Mount(r, opts.Config, opts.Manager); err != nil {
		return nil, err
	}

	dashboard, err := fs.Sub(web.Dashboard, "dashboard")
	if err != nil {
		return nil, err
	}
	r.Handle("/dashboard/*", http.StripPrefix("/dashboard/", http.FileServer(http.FS(dashboard))))
	r.Get("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard/", http.StatusFound)
	})

	return r, nil
}
