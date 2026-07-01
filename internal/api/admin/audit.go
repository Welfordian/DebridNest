package admin

const (
	ActionAddMagnet       = "torrent.add_magnet"
	ActionUploadTorrent   = "torrent.upload"
	ActionDeleteTorrent   = "torrent.delete"
	ActionBatchDelete     = "torrent.batch_delete"
	ActionRetryTorrent    = "torrent.retry"
	ActionPurgeTorrents   = "torrent.purge"
	ActionMaintenance     = "maintenance.cleanup"
	ActionSettingsPatch   = "settings.patch"
	ActionUserCreate      = "user.create"
	ActionUserDelete      = "user.delete"
	ActionUserRotateToken = "user.rotate_token"
)
