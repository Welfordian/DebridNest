package retention

import (
	"context"
	"log"
	"time"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/diskusage"
	"github.com/debridnest/debridnest/internal/torrent"
)

type Runner struct {
	cfg     config.Config
	manager *torrent.Manager
}

func NewRunner(cfg config.Config, manager *torrent.Manager) *Runner {
	return &Runner{cfg: cfg, manager: manager}
}

func (r *Runner) Start() {
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		r.run(context.Background())
		for range ticker.C {
			r.run(context.Background())
		}
	}()
}

func (r *Runner) run(ctx context.Context) {
	if r.cfg.RetentionDays > 0 {
		cutoff := time.Now().UTC().Add(-time.Duration(r.cfg.RetentionDays) * 24 * time.Hour)
		removed, err := r.manager.DeleteCompletedBefore(ctx, cutoff)
		if err != nil {
			log.Printf("retention: age cleanup: %v", err)
		} else if removed > 0 {
			log.Printf("retention: removed %d torrent(s) older than %d days", removed, r.cfg.RetentionDays)
		}
	}

	quota := r.cfg.DiskQuotaBytes()
	if quota <= 0 {
		return
	}

	used, err := diskusage.DirSize(r.manager.FilesDir())
	if err != nil {
		log.Printf("retention: disk usage: %v", err)
		return
	}
	if used <= quota {
		return
	}

	removed, err := r.manager.EvictOldestCompleted(ctx, used-quota)
	if err != nil {
		log.Printf("retention: quota eviction: %v", err)
		return
	}
	if removed > 0 {
		log.Printf("retention: evicted %d torrent(s) to satisfy disk quota", removed)
	}
}
