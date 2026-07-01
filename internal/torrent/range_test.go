package torrent

import "testing"

func TestParseRangeStart(t *testing.T) {
	size := int64(1000)
	tests := []struct {
		header string
		want   int64
	}{
		{"", 0},
		{"bytes=0-", 0},
		{"bytes=500-", 500},
		{"bytes=500-999", 500},
		{"bytes=-500", 0},
		{"bytes=1500-", 0},
		{"invalid", 0},
	}
	for _, tc := range tests {
		got := ParseRangeStart(tc.header, size)
		if got != tc.want {
			t.Fatalf("ParseRangeStart(%q) = %d, want %d", tc.header, got, tc.want)
		}
	}
}
