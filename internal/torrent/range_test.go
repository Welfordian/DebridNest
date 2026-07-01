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
		{"bytes=-500", 500},
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

func TestParseRange(t *testing.T) {
	size := int64(1000)
	tests := []struct {
		header string
		want   ByteRange
	}{
		{"", ByteRange{}},
		{"bytes=0-", ByteRange{Start: 0}},
		{"bytes=500-", ByteRange{Start: 500}},
		{"bytes=500-999", ByteRange{Start: 500, Length: 500}},
		{"bytes=500-1500", ByteRange{Start: 500, Length: 500}},
		{"bytes=-500", ByteRange{Start: 500, Length: 500}},
		{"bytes=-1500", ByteRange{Start: 0, Length: 1000}},
		{"bytes=1500-", ByteRange{}},
		{"bytes=bad-", ByteRange{}},
		{"invalid", ByteRange{}},
	}
	for _, tc := range tests {
		got := ParseRange(tc.header, size)
		if got != tc.want {
			t.Fatalf("ParseRange(%q) = %+v, want %+v", tc.header, got, tc.want)
		}
	}
}
