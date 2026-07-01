package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

const quotaWarnCooldown = time.Hour

type Service struct {
	settings SettingsReader
	client   *http.Client
	mu       sync.Mutex
	lastWarn time.Time
}

func New(settings SettingsReader) *Service {
	return &Service{
		settings: settings,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Service) NotifyDownloadComplete(torrentName string) {
	cfg := s.settings.NotifySettings()
	if !cfg.NotifyOnDownloadComplete {
		return
	}
	title := "Download complete"
	body := fmt.Sprintf("Torrent finished: %s", torrentName)
	s.sendAsync(title, body)
}

func (s *Service) NotifyQuotaWarning(used, quota int64) {
	cfg := s.settings.NotifySettings()
	if !cfg.NotifyOnQuotaWarning || quota <= 0 {
		return
	}

	s.mu.Lock()
	if !s.lastWarn.IsZero() && time.Since(s.lastWarn) < quotaWarnCooldown {
		s.mu.Unlock()
		return
	}
	s.lastWarn = time.Now()
	s.mu.Unlock()

	pct := used * 100 / quota
	title := "Disk quota warning"
	body := fmt.Sprintf("Disk usage at %d%% (%d / %d bytes)", pct, used, quota)
	s.sendAsync(title, body)
}

func (s *Service) sendAsync(title, body string) {
	cfg := s.settings.NotifySettings()
	go func() {
		if cfg.DiscordWebhookURL != "" {
			s.sendDiscord(cfg.DiscordWebhookURL, title, body)
		}
		if cfg.NtfyTopic != "" {
			s.sendNtfy(cfg.NtfyTopic, title, body)
		}
		if cfg.GotifyURL != "" {
			s.sendGotify(cfg.GotifyURL, cfg.GotifyToken, title, body)
		}
	}()
}

func (s *Service) sendDiscord(url, title, body string) {
	payload, err := json.Marshal(map[string]any{
		"content": fmt.Sprintf("**%s**\n%s", title, body),
	})
	if err != nil {
		return
	}
	s.post(url, payload, nil)
}

func (s *Service) sendNtfy(topic, title, body string) {
	url := "https://ntfy.sh/" + topic
	headers := map[string]string{
		"Title": title,
	}
	s.post(url, []byte(body), headers)
}

func (s *Service) sendGotify(baseURL, token, title, body string) {
	payload, err := json.Marshal(map[string]any{
		"title":   title,
		"message": body,
		"priority": 5,
	})
	if err != nil {
		return
	}
	headers := map[string]string{}
	if token != "" {
		headers["X-Gotify-Key"] = token
	}
	s.post(baseURL+"/message", payload, headers)
}

func (s *Service) post(url string, body []byte, headers map[string]string) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("notify: build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("notify: send: %v", err)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("notify: %s returned %s", url, resp.Status)
	}
}
