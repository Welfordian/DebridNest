package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/metrics"
	"github.com/debridnest/debridnest/internal/retention"
	"github.com/debridnest/debridnest/internal/server"
	"github.com/debridnest/debridnest/internal/storage"
	"github.com/debridnest/debridnest/internal/torrent"
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

	retentionRunner := retention.NewRunner(cfg, manager)
	retentionRunner.Start()

	var collector *metrics.Collector
	if cfg.MetricsEnabled {
		collector = metrics.New()
		collector.StartStatsCollector(context.Background(), manager, 15*time.Second)
	}

	r, err := server.NewRouter(server.Options{
		Config:          cfg,
		Manager:         manager,
		Signer:          signer,
		Metrics:         collector,
		RetentionRunner: retentionRunner,
	})
	if err != nil {
		log.Fatalf("router: %v", err)
	}

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
