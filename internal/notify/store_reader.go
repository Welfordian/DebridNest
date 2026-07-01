package notify

import (
	"github.com/debridnest/debridnest/internal/settings"
)

type StoreReader struct {
	Store *settings.Store
}

func (r StoreReader) NotifySettings() Settings {
	if r.Store == nil {
		return Settings{}
	}
	merged := r.Store.GetMerged()
	return Settings{
		DiscordWebhookURL:        merged.WebhookDiscordUrl,
		NtfyTopic:                merged.WebhookNtfyTopic,
		GotifyURL:                merged.WebhookGotifyUrl,
		GotifyToken:              merged.WebhookGotifyToken,
		NotifyOnDownloadComplete: merged.NotifyOnDownloadComplete,
		NotifyOnQuotaWarning:     merged.NotifyOnQuotaWarning,
	}
}
