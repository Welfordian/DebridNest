package qbit

import (
	"strings"
	"time"

	"github.com/debridnest/debridnest/internal/storage"
	torrentmgr "github.com/debridnest/debridnest/internal/torrent"
)

const (
	appVersion    = "v4.6.0"
	webAPIVersion = "2.11.0"
	defaultSave   = "/downloads"
)

func mapStatus(rec *storage.TorrentRecord) string {
	switch rec.Status {
	case string(torrentmgr.StatusMagnetConversion), string(torrentmgr.StatusWaitingFileSelection):
		return "metaDL"
	case string(torrentmgr.StatusQueued):
		return "queuedDL"
	case string(torrentmgr.StatusDownloading):
		if rec.Speed == 0 && rec.Progress < 100 {
			return "stalledDL"
		}
		return "downloading"
	case string(torrentmgr.StatusDownloaded):
		return "pausedUP"
	case string(torrentmgr.StatusError), string(torrentmgr.StatusMagnetError), string(torrentmgr.StatusDead):
		return "error"
	default:
		return "downloading"
	}
}

func normalizeHash(hash string) string {
	hash = strings.TrimSpace(strings.ToLower(hash))
	hash = strings.TrimPrefix(hash, "urn:btih:")
	if len(hash) == 40 {
		return hash
	}
	return ""
}

func torrentSize(rec *storage.TorrentRecord) int64 {
	if rec.Bytes > 0 {
		return rec.Bytes
	}
	return rec.OriginalBytes
}

func downloadedBytes(rec *storage.TorrentRecord) int64 {
	total := torrentSize(rec)
	if total <= 0 {
		return 0
	}
	return int64(float64(total) * float64(rec.Progress) / 100)
}

func amountLeft(rec *storage.TorrentRecord) int64 {
	total := torrentSize(rec)
	done := downloadedBytes(rec)
	if total > done {
		return total - done
	}
	return 0
}

func etaSeconds(rec *storage.TorrentRecord) int64 {
	left := amountLeft(rec)
	if rec.Speed <= 0 || left <= 0 {
		return 8640000
	}
	return left / rec.Speed
}

func toQBitTorrent(rec *storage.TorrentRecord, category string) map[string]any {
	hash := normalizeHash(rec.InfoHash)
	progress := float64(rec.Progress) / 100
	if torrentmgr.IsCompletedStatus(rec.Status) {
		progress = 1
	}

	size := torrentSize(rec)
	completed := downloadedBytes(rec)
	savePath := defaultSave
	contentPath := savePath + "/" + rec.Name

	return map[string]any{
		"added_on":           rec.AddedAt.Unix(),
		"amount_left":        amountLeft(rec),
		"auto_tmm":           false,
		"availability":       1,
		"category":           category,
		"completed":          completed,
		"completion_on":      completionOn(rec),
		"content_path":       contentPath,
		"dlspeed":            rec.Speed,
		"downloaded":         completed,
		"downloaded_session": completed,
		"eta":                etaSeconds(rec),
		"f_l_piece_prio":     false,
		"force_start":        false,
		"hash":               hash,
		"last_activity":      time.Now().Unix(),
		"magnet_uri":         rec.Magnet,
		"max_ratio":          -1,
		"max_seeding_time":   -1,
		"name":               rec.Name,
		"num_complete":       rec.Seeders,
		"num_incomplete":     -1,
		"num_leechs":         0,
		"num_seeds":          rec.Seeders,
		"priority":           0,
		"progress":           progress,
		"ratio":              0,
		"ratio_limit":        -2,
		"save_path":          savePath,
		"seeding_time_limit": -2,
		"seen_complete":      -1,
		"seq_dl":             false,
		"size":               size,
		"state":              mapStatus(rec),
		"super_seeding":      false,
		"tags":               "",
		"time_active":        timeActive(rec),
		"total_size":         rec.OriginalBytes,
		"tracker":            "",
		"upspeed":            0,
		"uploaded":           0,
		"uploaded_session":   0,
	}
}

func torrentProperties(rec *storage.TorrentRecord) map[string]any {
	size := torrentSize(rec)
	completed := downloadedBytes(rec)
	piecesHave := 0
	if torrentmgr.IsCompletedStatus(rec.Status) || completed >= size && size > 0 {
		piecesHave = 1
	}
	return map[string]any{
		"save_path":                defaultSave,
		"creation_date":            rec.AddedAt.Unix(),
		"piece_size":               size,
		"comment":                  "",
		"total_wasted":             int64(0),
		"total_uploaded":           int64(0),
		"total_uploaded_session":   int64(0),
		"total_downloaded":         completed,
		"total_downloaded_session": completed,
		"up_limit":                 int64(0),
		"dl_limit":                 int64(0),
		"time_elapsed":             timeActive(rec),
		"seeding_time":             int64(0),
		"nb_connections":           0,
		"nb_connections_limit":     -1,
		"share_ratio":              float64(0),
		"addition_date":            rec.AddedAt.Unix(),
		"completion_date":          completionOn(rec),
		"created_by":               "DebridNest",
		"dl_speed":                 rec.Speed,
		"dl_speed_avg":             int64(0),
		"eta":                      etaSeconds(rec),
		"last_seen":                int64(-1),
		"peers":                    0,
		"peers_total":              0,
		"pieces_have":              piecesHave,
		"pieces_num":               1,
		"reannounce":               int64(-1),
		"seeds":                    rec.Seeders,
		"seeds_total":              rec.Seeders,
		"total_size":               size,
	}
}

func qbitFilePriority(f storage.TorrentFileRecord) int {
	if !f.Selected {
		return 0
	}
	return 1
}

func completionOn(rec *storage.TorrentRecord) int64 {
	if rec.EndedAt != nil {
		return rec.EndedAt.Unix()
	}
	return -1
}

func timeActive(rec *storage.TorrentRecord) int64 {
	end := time.Now()
	if rec.EndedAt != nil {
		end = *rec.EndedAt
	}
	if end.Before(rec.AddedAt) {
		return 0
	}
	return int64(end.Sub(rec.AddedAt).Seconds())
}

func matchesFilter(state, filter string) bool {
	if filter == "" || filter == "all" {
		return true
	}
	switch filter {
	case "downloading":
		return state == "downloading" || state == "metaDL" || state == "queuedDL" || state == "stalledDL" || state == "checkingDL"
	case "completed":
		return state == "pausedUP" || state == "uploading" || state == "stalledUP" || state == "queuedUP"
	case "active":
		return state == "downloading" || state == "metaDL" || state == "stalledDL"
	case "inactive":
		return state == "pausedUP" || state == "error"
	case "paused":
		return state == "pausedUP" || state == "pausedDL"
	case "stalled":
		return state == "stalledDL" || state == "stalledUP"
	case "stalled_downloading":
		return state == "stalledDL"
	case "errored":
		return state == "error"
	default:
		return true
	}
}
