package torrent

import (
	"testing"
	"time"

	"github.com/debridnest/debridnest/internal/storage"
)

func TestLifecycleCountsByGroup(t *testing.T) {
	counts := LifecycleCountsByGroup(map[string]int{
		string(StatusDownloaded):           2,
		string(StatusError):                1,
		string(StatusDead):                 1,
		string(StatusMagnetError):          1,
		string(StatusDownloading):          3,
		string(StatusQueued):               4,
		string(StatusWaitingFileSelection): 5,
		string(StatusMagnetConversion):     6,
		"unknown":                          7,
	})

	if counts[LifecycleGroupCompleted] != 2 {
		t.Fatalf("completed count = %d, want 2", counts[LifecycleGroupCompleted])
	}
	if counts[LifecycleGroupFailed] != 3 {
		t.Fatalf("failed count = %d, want 3", counts[LifecycleGroupFailed])
	}
	if counts[LifecycleGroupActive] != 18 {
		t.Fatalf("active count = %d, want 18", counts[LifecycleGroupActive])
	}
	if counts[LifecycleGroupOther] != 7 {
		t.Fatalf("other count = %d, want 7", counts[LifecycleGroupOther])
	}
}

func TestLifecycleApplySelectionQueuesSelectedFiles(t *testing.T) {
	rec := &storage.TorrentRecord{
		Status: string(StatusWaitingFileSelection),
		Files: []storage.TorrentFileRecord{
			{ID: 1, Path: "/sample.txt", Bytes: 100},
			{ID: 2, Path: "/movie.mkv", Bytes: 900},
			{ID: 3, Path: "/extra.mp4", Bytes: 400},
		},
	}

	if err := NewLifecycle(8).ApplySelection(rec, "2,3"); err != nil {
		t.Fatalf("ApplySelection returned error: %v", err)
	}
	if rec.Status != string(StatusQueued) {
		t.Fatalf("status = %q, want queued", rec.Status)
	}
	if rec.Bytes != 1300 {
		t.Fatalf("bytes = %d, want 1300", rec.Bytes)
	}
	if rec.Files[0].Selected || !rec.Files[1].Selected || !rec.Files[2].Selected {
		t.Fatalf("selection = %+v, want only files 2 and 3 selected", rec.Files)
	}
}

func TestLifecycleApplySelectionRejectsInvalidSpec(t *testing.T) {
	rec := &storage.TorrentRecord{
		Files: []storage.TorrentFileRecord{{ID: 1, Bytes: 100}},
	}
	if err := NewLifecycle(8).ApplySelection(rec, "2"); err == nil {
		t.Fatal("ApplySelection returned nil error, want invalid file id")
	}
}

func TestLifecyclePickSingleObviousVideo(t *testing.T) {
	l := NewLifecycle(8)
	if got := l.PickSingleObviousVideo([]storage.TorrentFileRecord{
		{ID: 1, Path: "/sample.txt", Bytes: 100},
		{ID: 2, Path: "/movie.mkv", Bytes: 900},
	}); got != 2 {
		t.Fatalf("PickSingleObviousVideo = %d, want 2", got)
	}
	if got := l.PickSingleObviousVideo([]storage.TorrentFileRecord{
		{ID: 1, Path: "/episode-1.mkv", Bytes: 100},
		{ID: 2, Path: "/episode-2.mkv", Bytes: 900},
	}); got != 0 {
		t.Fatalf("PickSingleObviousVideo ambiguous = %d, want 0", got)
	}
}

func TestLifecycleRuntimeTransitions(t *testing.T) {
	l := NewLifecycle(8)
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	pending := &storage.TorrentRecord{Status: string(StatusMagnetConversion)}
	l.ApplyRuntimeSnapshot(pending, RuntimeSnapshot{
		TotalBytes:     100,
		CompletedBytes: 50,
		Seeders:        3,
		Now:            now,
	})
	if pending.Status != string(StatusMagnetConversion) {
		t.Fatalf("pending status = %q, want magnet_conversion", pending.Status)
	}
	if pending.Progress != 50 || pending.Seeders != 3 {
		t.Fatalf("pending progress/seeders = %d/%d, want 50/3", pending.Progress, pending.Seeders)
	}

	active := &storage.TorrentRecord{Status: string(StatusQueued)}
	l.ApplyRuntimeSnapshot(active, RuntimeSnapshot{
		TotalBytes:     100,
		CompletedBytes: 50,
		Now:            now,
	})
	if active.Status != string(StatusDownloading) {
		t.Fatalf("active status = %q, want downloading", active.Status)
	}

	completed := &storage.TorrentRecord{Status: string(StatusDownloading)}
	l.ApplyRuntimeSnapshot(completed, RuntimeSnapshot{
		TotalBytes:     100,
		CompletedBytes: 100,
		Now:            now,
	})
	if completed.Status != string(StatusDownloaded) || completed.Progress != 100 {
		t.Fatalf("completed status/progress = %q/%d, want downloaded/100", completed.Status, completed.Progress)
	}
	if completed.EndedAt == nil || !completed.EndedAt.Equal(now) {
		t.Fatalf("EndedAt = %v, want %v", completed.EndedAt, now)
	}
}

func TestLifecycleViewSeparatesStreamableFromCompleted(t *testing.T) {
	rec := &storage.TorrentRecord{
		Status: string(StatusDownloading),
		Files: []storage.TorrentFileRecord{
			{ID: 1, Selected: true, Path: "/movie.mkv", Bytes: 100, DownloadedBytes: 32, StreamableBytes: 32},
		},
	}

	view := NewLifecycle(16).View(rec)
	if !view.Streamable || !view.LinksVisible {
		t.Fatalf("streamable view = %+v, want streamable links visible", view)
	}
	if view.Completed {
		t.Fatalf("streamable downloading view marked completed: %+v", view)
	}
	if view.Tone != "streamable" {
		t.Fatalf("tone = %q, want streamable", view.Tone)
	}

	rec.Status = string(StatusDead)
	view = NewLifecycle(16).View(rec)
	if view.LinksVisible || !view.Failed {
		t.Fatalf("dead view = %+v, want failed with hidden links", view)
	}
}

func TestLifecycleRequiresContiguousStreamableBytes(t *testing.T) {
	rec := &storage.TorrentRecord{
		Status: string(StatusDownloading),
		Files: []storage.TorrentFileRecord{
			{ID: 1, Selected: true, Path: "/movie.mkv", Bytes: 100, DownloadedBytes: 64, StreamableBytes: 8},
		},
	}

	view := NewLifecycle(16).View(rec)
	if view.Streamable || view.LinksVisible {
		t.Fatalf("view = %+v, want not streamable when prefix bytes are below threshold", view)
	}

	rec.Files[0].StreamableBytes = 16
	view = NewLifecycle(16).View(rec)
	if !view.Streamable || !view.LinksVisible {
		t.Fatalf("view = %+v, want streamable when prefix bytes meet threshold", view)
	}
}

func TestLifecycleIgnoresNonVideoPrefixBytes(t *testing.T) {
	rec := &storage.TorrentRecord{
		Status: string(StatusDownloading),
		Files: []storage.TorrentFileRecord{
			{ID: 1, Selected: true, Path: "/sample.txt", Bytes: 100, DownloadedBytes: 100, StreamableBytes: 100},
		},
	}

	view := NewLifecycle(16).View(rec)
	if view.Streamable || view.LinksVisible {
		t.Fatalf("view = %+v, want non-video file not streamable", view)
	}
}

func TestLifecycleFileLinksVisible(t *testing.T) {
	l := NewLifecycle(16)
	rec := &storage.TorrentRecord{
		Status: string(StatusDownloading),
		Files: []storage.TorrentFileRecord{
			{ID: 1, Selected: true, Path: "/ready.mkv", Bytes: 100, StreamableBytes: 32},
			{ID: 2, Selected: true, Path: "/later.mkv", Bytes: 100, StreamableBytes: 8},
		},
	}

	if !l.FileLinksVisible(rec, rec.Files[0]) {
		t.Fatal("ready selected video file should expose links")
	}
	if l.FileLinksVisible(rec, rec.Files[1]) {
		t.Fatal("unready selected video file should not expose links")
	}

	rec.Status = string(StatusDownloaded)
	if !l.FileLinksVisible(rec, rec.Files[1]) {
		t.Fatal("completed selected file should expose links")
	}

	rec.Status = string(StatusMagnetError)
	if l.FileLinksVisible(rec, rec.Files[0]) {
		t.Fatal("failed torrent should not expose links")
	}
}
