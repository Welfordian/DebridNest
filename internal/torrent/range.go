package torrent

import (
	"strconv"
	"strings"
)

type ByteRange struct {
	Start  int64
	Length int64
}

func ParseRange(rangeHeader string, contentSize int64) ByteRange {
	if contentSize <= 0 || rangeHeader == "" {
		return ByteRange{}
	}
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return ByteRange{}
	}

	spec := strings.TrimPrefix(rangeHeader, "bytes=")
	part := strings.TrimSpace(strings.Split(spec, ",")[0])
	dash := strings.IndexByte(part, '-')
	if dash < 0 {
		return ByteRange{}
	}

	startStr := strings.TrimSpace(part[:dash])
	endStr := strings.TrimSpace(part[dash+1:])
	if startStr == "" {
		suffix, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || suffix <= 0 {
			return ByteRange{}
		}
		if suffix > contentSize {
			suffix = contentSize
		}
		return ByteRange{Start: contentSize - suffix, Length: suffix}
	}

	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil || start < 0 || start >= contentSize {
		return ByteRange{}
	}
	if endStr == "" {
		return ByteRange{Start: start}
	}

	end, err := strconv.ParseInt(endStr, 10, 64)
	if err != nil || end < start {
		return ByteRange{Start: start}
	}
	if end >= contentSize {
		end = contentSize - 1
	}
	return ByteRange{Start: start, Length: end - start + 1}
}

// ParseRangeStart returns the first byte offset from an HTTP Range header.
func ParseRangeStart(rangeHeader string, contentSize int64) int64 {
	return ParseRange(rangeHeader, contentSize).Start
}
