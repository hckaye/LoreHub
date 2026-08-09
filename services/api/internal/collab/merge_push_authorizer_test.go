package collab

import (
	"testing"
	"time"
)

func TestObservedBranchFreshnessFailsClosed(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{name: "current", at: now, want: true},
		{name: "under limit", at: now.Add(-observedBranchMaxAge + time.Second), want: true},
		{name: "at limit", at: now.Add(-observedBranchMaxAge), want: true},
		{name: "stale", at: now.Add(-observedBranchMaxAge - time.Second), want: false},
		{name: "future", at: now.Add(time.Second), want: false},
		{name: "missing", want: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := observedBranchFresh(test.at, now); got != test.want {
				t.Fatalf("observedBranchFresh(%v) = %t, want %t", test.at, got, test.want)
			}
		})
	}
}
