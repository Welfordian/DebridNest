package transcode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/storage"
)

func TestEnsureJobFailureEvictsAndCleansPartialOutput(t *testing.T) {
	dataDir := t.TempDir()
	rec, file := transcodeTestFile()
	var calls atomic.Int32

	h := &handler{
		cfg: config.Config{DataDir: dataDir},
		runHLS: func(ctx context.Context, job *hlsJob) error {
			call := calls.Add(1)
			if call == 1 {
				if err := os.WriteFile(filepath.Join(job.outDir, "seg000.ts"), []byte("partial"), 0o644); err != nil {
					return err
				}
				return errors.New("transcode failed")
			}
			return os.WriteFile(filepath.Join(job.outDir, "index.m3u8"), []byte("#EXTM3U\n"), 0o644)
		},
		jobTimeout: time.Minute,
	}

	job, err := h.ensureJob(rec, file)
	if err != nil {
		t.Fatalf("ensureJob failed: %v", err)
	}
	if err := job.waitReady(context.Background(), time.Second); err == nil || !strings.Contains(err.Error(), "transcode failed") {
		t.Fatalf("waitReady error = %v, want transcode failure", err)
	}

	key := transcodeTestJobKey(rec.ID, file.ID)
	if _, ok := h.jobs.Load(key); ok {
		t.Fatalf("failed job remained cached")
	}
	if _, err := os.Stat(filepath.Join(job.outDir, "seg000.ts")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial segment stat error = %v, want not exist", err)
	}

	retryJob, err := h.ensureJob(rec, file)
	if err != nil {
		t.Fatalf("retry ensureJob failed: %v", err)
	}
	if retryJob == job {
		t.Fatalf("retry reused failed job")
	}
	if err := retryJob.waitReady(context.Background(), time.Second); err != nil {
		t.Fatalf("retry waitReady failed: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("runner calls = %d, want 2", calls.Load())
	}
	if _, err := os.Stat(filepath.Join(retryJob.outDir, "index.m3u8")); err != nil {
		t.Fatalf("retry output missing: %v", err)
	}
}

func TestWaitCancellationDoesNotPoisonRunningJob(t *testing.T) {
	dataDir := t.TempDir()
	rec, file := transcodeTestFile()
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(release)
		})
	})

	h := &handler{
		cfg: config.Config{DataDir: dataDir},
		runHLS: func(ctx context.Context, job *hlsJob) error {
			close(started)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
				return os.WriteFile(filepath.Join(job.outDir, "index.m3u8"), []byte("#EXTM3U\n"), 0o644)
			}
		},
		jobTimeout: time.Minute,
	}

	job, err := h.ensureJob(rec, file)
	if err != nil {
		t.Fatalf("ensureJob failed: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("runner did not start")
	}

	waitCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := job.waitReady(waitCtx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitReady error = %v, want context canceled", err)
	}
	if _, ok := h.jobs.Load(transcodeTestJobKey(rec.ID, file.ID)); !ok {
		t.Fatalf("running job was evicted after waiter cancellation")
	}

	releaseOnce.Do(func() {
		close(release)
	})
	if err := job.waitReady(context.Background(), time.Second); err != nil {
		t.Fatalf("job failed after waiter cancellation: %v", err)
	}
	cachedJob, err := h.ensureJob(rec, file)
	if err != nil {
		t.Fatalf("ensureJob after success failed: %v", err)
	}
	if cachedJob != job {
		t.Fatalf("successful job was not retained")
	}
}

func TestJobTimeoutEvictsFailedJob(t *testing.T) {
	dataDir := t.TempDir()
	rec, file := transcodeTestFile()

	h := &handler{
		cfg: config.Config{DataDir: dataDir},
		runHLS: func(ctx context.Context, job *hlsJob) error {
			<-ctx.Done()
			return ctx.Err()
		},
		jobTimeout: 10 * time.Millisecond,
	}

	job, err := h.ensureJob(rec, file)
	if err != nil {
		t.Fatalf("ensureJob failed: %v", err)
	}
	if err := job.waitReady(context.Background(), time.Second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitReady error = %v, want deadline exceeded", err)
	}
	if _, ok := h.jobs.Load(transcodeTestJobKey(rec.ID, file.ID)); ok {
		t.Fatalf("timed-out job remained cached")
	}
}

func transcodeTestFile() (*storage.TorrentRecord, *storage.TorrentFileRecord) {
	rec := &storage.TorrentRecord{ID: "torrent-1"}
	file := &storage.TorrentFileRecord{
		ID:        7,
		TorrentID: rec.ID,
		Path:      "movie.mkv",
		Selected:  true,
		DiskPath:  "/tmp/movie.mkv",
	}
	return rec, file
}

func transcodeTestJobKey(torrentID string, fileID int) string {
	return torrentID + "/" + strconv.Itoa(fileID)
}
