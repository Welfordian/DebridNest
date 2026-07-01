package activity

import (
	"context"
	"encoding/json"
	"time"

	"github.com/debridnest/debridnest/internal/storage"
)

type Entry struct {
	ID        int64          `json:"id"`
	UserID    string         `json:"userId"`
	UserName  string         `json:"userName"`
	Action    string         `json:"action"`
	Details   map[string]any `json:"details"`
	CreatedAt time.Time      `json:"createdAt"`
}

type Service struct {
	db *storage.DB
}

func New(db *storage.DB) *Service {
	return &Service{db: db}
}

func (s *Service) Log(ctx context.Context, userID, userName, action string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO activity_log (user_id, user_name, action, details, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		userID, userName, action, string(raw), time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *Service) List(ctx context.Context, limit, offset int) ([]Entry, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, user_name, action, details, created_at
		FROM activity_log
		ORDER BY id DESC
		LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		var detailsRaw, created string
		if err := rows.Scan(&e.ID, &e.UserID, &e.UserName, &e.Action, &detailsRaw, &created); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		e.Details = map[string]any{}
		if detailsRaw != "" {
			_ = json.Unmarshal([]byte(detailsRaw), &e.Details)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
