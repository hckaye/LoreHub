package code

import "testing"

func TestHistoryWindowUsesSentinelOnlyForMoreRows(t *testing.T) {
	for _, test := range []struct {
		name    string
		entries int
		limit   int
		want    int
		hasMore bool
	}{
		{name: "empty", entries: 0, limit: 3, want: 0, hasMore: false},
		{name: "below limit", entries: 2, limit: 3, want: 2, hasMore: false},
		{name: "exact limit", entries: 3, limit: 3, want: 3, hasMore: false},
		{name: "sentinel row", entries: 4, limit: 3, want: 3, hasMore: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			entries := make([]int, test.entries)
			got, hasMore := historyWindow(entries, test.limit)
			if len(got) != test.want || hasMore != test.hasMore {
				t.Fatalf("historyWindow(%d, %d) = %d, %v; want %d, %v",
					test.entries, test.limit, len(got), hasMore, test.want, test.hasMore)
			}
		})
	}
}

func TestBoundedIntRejectsOverflowWithoutWrapping(t *testing.T) {
	if got := boundedInt("999999999999999999999999999999999999", 50, 500); got != 500 {
		t.Fatalf("boundedInt overflow = %d, want 500", got)
	}
	if got := boundedInt("0", 50, 500); got != 50 {
		t.Fatalf("boundedInt zero = %d, want fallback 50", got)
	}
}
