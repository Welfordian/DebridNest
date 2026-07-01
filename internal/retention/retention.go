package retention

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/diskusage"
	"github.com/debridnest/debridnest/internal/torrent"
)

type RetentionResult struct {
	AgeRemoved   int
	QuotaRemoved int
	DiskUsed     int64
	DiskQuota    int64
}

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

func (r *Runner) RunNow(ctx context.Context) (RetentionResult, error) {
	var result RetentionResult
	result.DiskQuota = r.cfg.DiskQuotaBytes()
	var errs []error

	if r.cfg.RetentionDays > 0 {
		cutoff := time.Now().UTC().Add(-time.Duration(r.cfg.RetentionDays) * 24 * time.Hour)
		removed, err := r.manager.DeleteCompletedBefore(ctx, cutoff)
		if err != nil {
			errs = append(errs, fmt.Errorf("age cleanup: %w", err))
		} else {
			result.AgeRemoved = removed
		}
	}

	used, err := diskusage.DirSize(r.manager.FilesDir())
	if err != nil {
		errs = append(errs, fmt.Errorf("disk usage: %w", err))
		return result, errors.Join(errs...)
	}
	result.DiskUsed = used

	quota := result.DiskQuota
	if quota <= 0 || used <= quota {
		return result, errors.Join(errs...)
	}

	removed, err := r.manager.EvictOldestCompleted(ctx, used-quota)
	if err != nil {
		errs = append(errs, fmt.Errorf("quota eviction: %w", err))
		return result, errors.Join(errs...)
	}
	result.QuotaRemoved = removed

	if usedAfter, err := diskusage.DirSize(r.manager.FilesDir()); err == nil {
		result.DiskUsed = usedAfter
	}
	return result, errors.Join(errs...)
}

func (r *Runner) run(ctx context.Context) {
	result, err := r.RunNow(ctx)
	if err != nil {
		log.Printf("retention: %v", err)
	}
	if result.AgeRemoved > 0 {
		log.Printf("retention: removed %d torrent(s) older than %d days", result.AgeRemoved, r.cfg.RetentionDays)
	}
	if result.QuotaRemoved > 0 {
		log.Printf("retention: evicted %d torrent(s) to satisfy disk quota", result.QuotaRemoved)
	}
}
