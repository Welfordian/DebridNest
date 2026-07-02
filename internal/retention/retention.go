package retention

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/diskusage"
	"github.com/debridnest/debridnest/internal/settings"
	"github.com/debridnest/debridnest/internal/torrent"
)

type RetentionResult struct {
	AgeRemoved     int
	QuotaRemoved   int
	S3QuotaRemoved int
	DiskUsed       int64
	DiskQuota      int64
	S3Used         int64
	S3Quota        int64
}

type Runner struct {
	cfg            config.Config
	manager        *torrent.Manager
	settings       *settings.Store
	onQuotaWarning func(used, quota int64)
}

func NewRunner(cfg config.Config, manager *torrent.Manager, settingsStore *settings.Store) *Runner {
	return &Runner{cfg: cfg, manager: manager, settings: settingsStore}
}

func (r *Runner) SetQuotaWarningHook(fn func(used, quota int64)) {
	r.onQuotaWarning = fn
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
	result.DiskQuota = r.diskQuotaBytes()
	result.S3Quota = r.objectStorageQuotaBytes()
	var errs []error

	retentionDays := r.retentionDays()
	if retentionDays > 0 {
		cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)
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
	if quota > 0 && used*100/quota >= 90 {
		if r.onQuotaWarning != nil {
			r.onQuotaWarning(used, quota)
		}
	}

	if quota > 0 && used > quota {
		removed, err := r.manager.EvictOldestCompleted(ctx, used-quota)
		if err != nil {
			errs = append(errs, fmt.Errorf("quota eviction: %w", err))
			return result, errors.Join(errs...)
		}
		result.QuotaRemoved = removed

		if usedAfter, err := diskusage.DirSize(r.manager.FilesDir()); err == nil {
			result.DiskUsed = usedAfter
		}
	}

	objectUsage, err := r.manager.ObjectStorageUsage(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("object storage usage: %w", err))
		return result, errors.Join(errs...)
	}
	result.S3Used = objectUsage.Bytes
	if result.S3Quota > 0 && result.S3Used > result.S3Quota {
		removed, err := r.manager.EvictOldestRemoteStored(ctx, result.S3Used-result.S3Quota)
		if err != nil {
			errs = append(errs, fmt.Errorf("object storage quota eviction: %w", err))
		}
		result.S3QuotaRemoved = removed
		if after, err := r.manager.ObjectStorageUsage(ctx); err == nil {
			result.S3Used = after.Bytes
		}
		if result.S3Used > result.S3Quota {
			errs = append(errs, fmt.Errorf("object storage quota still exceeded after eviction: %d used / %d quota", result.S3Used, result.S3Quota))
			return result, errors.Join(errs...)
		}
	}

	return result, errors.Join(errs...)
}

func (r *Runner) retentionDays() int {
	if r.settings != nil {
		return r.settings.GetRetentionDays()
	}
	return r.cfg.RetentionDays
}

func (r *Runner) diskQuotaBytes() int64 {
	if r.settings != nil {
		return r.settings.DiskQuotaBytes()
	}
	return r.cfg.DiskQuotaBytes()
}

func (r *Runner) objectStorageQuotaBytes() int64 {
	if r.settings != nil {
		if !r.settings.GetS3Enabled() {
			return 0
		}
		return r.settings.S3QuotaBytes()
	}
	return r.manager.ObjectStorageQuotaBytes()
}

func (r *Runner) run(ctx context.Context) {
	result, err := r.RunNow(ctx)
	if err != nil {
		log.Printf("retention: %v", err)
	}
	if result.AgeRemoved > 0 {
		log.Printf("retention: removed %d torrent(s) older than %d days", result.AgeRemoved, r.retentionDays())
	}
	if result.QuotaRemoved > 0 {
		log.Printf("retention: evicted %d torrent(s) to satisfy disk quota", result.QuotaRemoved)
	}
	if result.S3QuotaRemoved > 0 {
		log.Printf("retention: evicted %d torrent(s) to satisfy object storage quota", result.S3QuotaRemoved)
	}
}
