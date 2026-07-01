package nzbget_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/debridnest/debridnest/internal/nzbget"
)

func TestAppendContent(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jsonrpc" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.Method != "append" {
			t.Fatalf("method = %q", req.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":42}`))
	}))
	defer srv.Close()

	client, err := nzbget.New(srv.URL, "nzbget", "secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	id, err := client.AppendContent(context.Background(), "test.nzb", []byte("<nzb></nzb>"), "debridnest")
	if err != nil {
		t.Fatalf("AppendContent: %v", err)
	}
	if id != 42 {
		t.Fatalf("id = %d, want 42", id)
	}
}
