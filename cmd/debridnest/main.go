package main

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/debridnest/debridnest/internal/api/admin"
	"github.com/debridnest/debridnest/internal/api/rd"
	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/retention"
	"github.com/debridnest/debridnest/internal/storage"
	"github.com/debridnest/debridnest/internal/torrent"
	"github.com/debridnest/debridnest/internal/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	db, err := storage.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer db.Close()

	signer := links.NewSigner(cfg.LinkSecret, cfg.PublicURL, cfg.Host, cfg.LinkTTL)
	manager, err := torrent.NewManager(cfg, db, signer)
	if err != nil {
		log.Fatalf("torrent: %v", err)
	}
	defer manager.Close()

	retention.NewRunner(cfg, manager).Start()

	rdHandler := rd.NewHandler(cfg, manager, signer)
	rdRoutes := rdHandler.Routes()
	adminHandler := admin.NewHandler(cfg, manager)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Mount("/rest/1.0", rdRoutes)
	r.Mount("/api/v1", adminHandler.Routes())
	r.Get("/d/{linkID}", func(w http.ResponseWriter, req *http.Request) {
		req.URL.Path = "/d/" + chi.URLParam(req, "linkID")
		rdRoutes.ServeHTTP(w, req)
	})
	r.Handle("/dl/*", http.HandlerFunc(rdHandler.ServeDownloadPublic))

	dashboard, err := fs.Sub(web.Dashboard, "dashboard")
	if err != nil {
		log.Fatalf("dashboard embed: %v", err)
	}
	r.Handle("/dashboard/*", http.StripPrefix("/dashboard/", http.FileServer(http.FS(dashboard))))
	r.Get("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard/", http.StatusFound)
	})

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("debridnest listening on %s", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
