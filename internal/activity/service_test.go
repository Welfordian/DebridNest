package activity

import (
	"context"
	"testing"
)

func TestLogAndList(t *testing.T) {
	dir := t.TempDir()
	db, err := openTestDB(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	svc := New(db)
	ctx := context.Background()

	if err := svc.Log(ctx, "u1", "alice", "torrent.add_magnet", map[string]any{"torrentId": "ABC123"}); err != nil {
		t.Fatalf("log: %v", err)
	}

	items, err := svc.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len = %d, want 1", len(items))
	}
	if items[0].UserName != "alice" || items[0].Action != "torrent.add_magnet" {
		t.Fatalf("unexpected entry: %+v", items[0])
	}
	if items[0].Details["torrentId"] != "ABC123" {
		t.Fatalf("details = %v", items[0].Details)
	}
}
