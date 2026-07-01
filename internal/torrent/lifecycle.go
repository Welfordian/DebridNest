package torrent

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/debridnest/debridnest/internal/storage"
)

type Status string

const (
	StatusMagnetConversion     Status = "magnet_conversion"
	StatusWaitingFileSelection Status = "waiting_files_selection"
	StatusQueued               Status = "queued"
	StatusDownloading          Status = "downloading"
	StatusDownloaded           Status = "downloaded"
	StatusError                Status = "error"
	StatusMagnetError          Status = "magnet_error"
	StatusDead                 Status = "dead"
)

const (
	LifecycleGroupActive    = "active"
	LifecycleGroupCompleted = "completed"
	LifecycleGroupFailed    = "failed"
	LifecycleGroupOther     = "other"
)

type Lifecycle struct {
	minStreamBytes int64
}

type RuntimeSnapshot struct {
	TotalBytes     int64
	CompletedBytes int64
	Seeders        int
	Now            time.Time
}

type LifecycleView struct {
	Status       string `json:"status"`
	Group        string `json:"group"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	Tone         string `json:"tone"`
	Active       bool   `json:"active"`
	Completed    bool   `json:"completed"`
	Failed       bool   `json:"failed"`
	Streamable   bool   `json:"streamable"`
	LinksVisible bool   `json:"linksVisible"`
	SortRank     int    `json:"sortRank"`
}

func NewLifecycle(minStreamBytes int64) Lifecycle {
	return Lifecycle{minStreamBytes: minStreamBytes}
}

func NormalizeStatus(status string) Status {
	return Status(strings.TrimSpace(status))
}

func (l Lifecycle) Streamable(rec *storage.TorrentRecord) bool {
	if rec == nil || l.minStreamBytes <= 0 {
		return false
	}
	for _, f := range rec.Files {
		if l.FileStreamable(f) {
			return true
		}
	}
	return false
}

func (l Lifecycle) FileStreamable(f storage.TorrentFileRecord) bool {
	return l.minStreamBytes > 0 && f.Selected && isVideoPath(f.Path) && f.StreamableBytes >= l.minStreamBytes
}

func (l Lifecycle) LinksVisible(rec *storage.TorrentRecord) bool {
	if rec == nil || IsFailedStatus(rec.Status) {
		return false
	}
	if IsCompletedStatus(rec.Status) {
		return true
	}
	for _, f := range rec.Files {
		if l.FileLinksVisible(rec, f) {
			return true
		}
	}
	return false
}

func (l Lifecycle) FileLinksVisible(rec *storage.TorrentRecord, f storage.TorrentFileRecord) bool {
	if rec == nil || !f.Selected || IsFailedStatus(rec.Status) {
		return false
	}
	if IsCompletedStatus(rec.Status) {
		return true
	}
	return l.FileStreamable(f)
}

func (l Lifecycle) ApplyRuntimeSnapshot(rec *storage.TorrentRecord, snap RuntimeSnapshot) {
	if rec == nil {
		return
	}
	if snap.Now.IsZero() {
		snap.Now = time.Now().UTC()
	}
	if snap.TotalBytes > 0 {
		rec.Progress = int(snap.CompletedBytes * 100 / snap.TotalBytes)
	}
	if snap.CompletedBytes >= snap.TotalBytes && snap.TotalBytes > 0 {
		l.MarkDownloaded(rec, snap.Now)
	} else if !IsPendingMetadataStatus(rec.Status) && !IsWaitingSelectionStatus(rec.Status) {
		rec.Status = string(StatusDownloading)
	}
	rec.Seeders = snap.Seeders
}

func (l Lifecycle) ApplySelection(rec *storage.TorrentRecord, filesSpec string) error {
	if rec == nil {
		return fmt.Errorf("unknown torrent")
	}
	selectedIDs, err := parseFilesSpec(filesSpec, len(rec.Files))
	if err != nil {
		return err
	}

	selectedSet := map[int]bool{}
	for _, id := range selectedIDs {
		selectedSet[id] = true
	}

	var selectedBytes int64
	for i := range rec.Files {
		rec.Files[i].Selected = selectedSet[rec.Files[i].ID]
		if rec.Files[i].Selected {
			selectedBytes += rec.Files[i].Bytes
		} else {
			rec.Files[i].StreamableBytes = 0
		}
	}
	rec.Bytes = selectedBytes
	rec.Status = string(StatusQueued)
	return nil
}

func (l Lifecycle) PickLargestVideo(files []storage.TorrentFileRecord) int {
	var bestID int
	var bestSize int64
	for _, f := range files {
		if !isVideoPath(f.Path) {
			continue
		}
		if f.Bytes > bestSize {
			bestSize = f.Bytes
			bestID = f.ID
		}
	}
	return bestID
}

func (l Lifecycle) PickSingleObviousVideo(files []storage.TorrentFileRecord) int {
	var videoID int
	for _, f := range files {
		if !isVideoPath(f.Path) {
			continue
		}
		if videoID != 0 {
			return 0
		}
		videoID = f.ID
	}
	return videoID
}

func isVideoPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mkv", ".mp4", ".avi", ".m4v", ".webm", ".mov", ".wmv", ".flv", ".ts", ".m2ts":
		return true
	default:
		return false
	}
}

func (l Lifecycle) MarkDownloaded(rec *storage.TorrentRecord, now time.Time) {
	if rec == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rec.Status = string(StatusDownloaded)
	rec.Progress = 100
	for i := range rec.Files {
		if rec.Files[i].Selected {
			rec.Files[i].StreamableBytes = rec.Files[i].Bytes
		}
	}
	if rec.EndedAt == nil {
		ended := now.UTC()
		rec.EndedAt = &ended
	}
}

func (l Lifecycle) View(rec *storage.TorrentRecord) LifecycleView {
	status := ""
	if rec != nil {
		status = rec.Status
	}
	return lifecycleView(status, l.Streamable(rec), rec != nil && l.LinksVisible(rec))
}

func (l Lifecycle) CountsByGroup(statusCounts map[string]int) map[string]int {
	out := map[string]int{
		LifecycleGroupActive:    0,
		LifecycleGroupCompleted: 0,
		LifecycleGroupFailed:    0,
		LifecycleGroupOther:     0,
	}
	for status, count := range statusCounts {
		out[StatusGroup(status)] += count
	}
	return out
}

func LifecycleViewForRecord(rec *storage.TorrentRecord, minBytes int64) LifecycleView {
	return NewLifecycle(minBytes).View(rec)
}

func LifecycleCountsByGroup(statusCounts map[string]int) map[string]int {
	return NewLifecycle(0).CountsByGroup(statusCounts)
}

func parseFilesSpec(spec string, fileCount int) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("empty files spec")
	}
	if strings.EqualFold(spec, "all") {
		ids := make([]int, fileCount)
		for i := range ids {
			ids[i] = i + 1
		}
		return ids, nil
	}
	parts := strings.Split(spec, ",")
	var ids []int
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var id int
		if _, err := fmt.Sscanf(part, "%d", &id); err != nil || id < 1 || id > fileCount {
			return nil, fmt.Errorf("invalid file id: %s", part)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func IsPendingMetadataStatus(status string) bool {
	return NormalizeStatus(status) == StatusMagnetConversion
}

func IsWaitingSelectionStatus(status string) bool {
	return NormalizeStatus(status) == StatusWaitingFileSelection
}

func IsActiveStatus(status string) bool {
	switch NormalizeStatus(status) {
	case StatusMagnetConversion, StatusWaitingFileSelection, StatusQueued, StatusDownloading:
		return true
	default:
		return false
	}
}

func IsCompletedStatus(status string) bool {
	return NormalizeStatus(status) == StatusDownloaded
}

func IsFailedStatus(status string) bool {
	switch NormalizeStatus(status) {
	case StatusError, StatusMagnetError, StatusDead:
		return true
	default:
		return false
	}
}

func IsDeadStatus(status string) bool {
	return NormalizeStatus(status) == StatusDead
}

func StatusGroup(status string) string {
	switch {
	case IsActiveStatus(status):
		return LifecycleGroupActive
	case IsCompletedStatus(status):
		return LifecycleGroupCompleted
	case IsFailedStatus(status):
		return LifecycleGroupFailed
	default:
		return LifecycleGroupOther
	}
}

func lifecycleView(status string, streamable bool, linksVisible bool) LifecycleView {
	group := StatusGroup(status)
	view := LifecycleView{
		Status:       status,
		Group:        group,
		Label:        statusLabel(status),
		Description:  statusDescription(status, streamable),
		Tone:         statusTone(group, status, streamable),
		Active:       group == LifecycleGroupActive,
		Completed:    group == LifecycleGroupCompleted,
		Failed:       group == LifecycleGroupFailed,
		Streamable:   streamable,
		LinksVisible: linksVisible,
		SortRank:     statusSortRank(status, streamable),
	}
	return view
}

func statusLabel(status string) string {
	switch NormalizeStatus(status) {
	case StatusMagnetConversion:
		return "Reading metadata"
	case StatusWaitingFileSelection:
		return "Selecting files"
	case StatusQueued:
		return "Queued"
	case StatusDownloading:
		return "Downloading"
	case StatusDownloaded:
		return "Ready"
	case StatusError:
		return "Error"
	case StatusMagnetError:
		return "Magnet failed"
	case StatusDead:
		return "Dead"
	default:
		if strings.TrimSpace(status) == "" {
			return "Unknown"
		}
		return strings.ReplaceAll(status, "_", " ")
	}
}

func statusDescription(status string, streamable bool) string {
	if streamable && !IsCompletedStatus(status) {
		return "Stream can start while the download continues."
	}
	switch NormalizeStatus(status) {
	case StatusMagnetConversion:
		return "Resolving torrent metadata and file list."
	case StatusWaitingFileSelection:
		return "Metadata is ready and files are being selected."
	case StatusQueued:
		return "Files are selected and waiting for peers."
	case StatusDownloading:
		return "Selected files are downloading."
	case StatusDownloaded:
		return "Selected files are complete and ready."
	case StatusError:
		return "The download failed."
	case StatusMagnetError:
		return "Magnet metadata could not be resolved."
	case StatusDead:
		return "The download is not recoverable."
	default:
		return "Lifecycle state is not classified."
	}
}

func statusTone(group string, status string, streamable bool) string {
	if streamable && !IsCompletedStatus(status) {
		return "streamable"
	}
	switch group {
	case LifecycleGroupActive:
		return "active"
	case LifecycleGroupCompleted:
		return "success"
	case LifecycleGroupFailed:
		return "danger"
	default:
		return "muted"
	}
}

func statusSortRank(status string, streamable bool) int {
	if streamable && !IsCompletedStatus(status) {
		return 10
	}
	switch NormalizeStatus(status) {
	case StatusDownloading:
		return 20
	case StatusQueued:
		return 30
	case StatusWaitingFileSelection:
		return 40
	case StatusMagnetConversion:
		return 50
	case StatusDownloaded:
		return 60
	case StatusError, StatusMagnetError, StatusDead:
		return 90
	default:
		return 80
	}
}
