package activity

import (
	"github.com/debridnest/debridnest/internal/storage"
)

func openTestDB(dir string) (*storage.DB, error) {
	return storage.Open(dir)
}
