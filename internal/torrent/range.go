package torrent

import (
	"strconv"
	"strings"
)

// ParseRangeStart returns the first byte offset from an HTTP Range header.
func ParseRangeStart(rangeHeader string, contentSize int64) int64 {
	if contentSize <= 0 || rangeHeader == "" {
		return 0
	}
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0
	}

	spec := strings.TrimPrefix(rangeHeader, "bytes=")
	part := strings.TrimSpace(strings.Split(spec, ",")[0])
	dash := strings.IndexByte(part, '-')
	if dash < 0 {
		return 0
	}

	startStr := strings.TrimSpace(part[:dash])
	if startStr == "" {
		// Suffix range (bytes=-N): treat as start at beginning for prioritization.
		return 0
	}

	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil || start < 0 || start >= contentSize {
		return 0
	}
	return start
}
