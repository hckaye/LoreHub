package code

import (
	"context"
	"errors"
	"testing"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type revisionAuthorLookup struct {
	users map[string]platform.User
	calls map[string]int
}

func (lookup *revisionAuthorLookup) ActiveUser(_ context.Context, userID string) (platform.User, error) {
	lookup.calls[userID]++
	user, ok := lookup.users[userID]
	if !ok {
		return platform.User{}, errors.New("user not found")
	}
	return user, nil
}

func TestResolveRevisionAuthorsUsesUsernamesAndCachesLookups(t *testing.T) {
	lookup := &revisionAuthorLookup{
		users: map[string]platform.User{"user-1": {Username: "alice"}},
		calls: make(map[string]int),
	}
	api := &API{users: lookup}
	entries := []loreclient.RevisionHistoryEntry{
		{Author: "user-1"},
		{Author: "user-1"},
		{Author: "missing-user"},
	}

	api.resolveRevisionAuthors(context.Background(), entries)

	if entries[0].Author != "alice" || entries[1].Author != "alice" {
		t.Fatalf("resolved authors = %q, %q; want alice", entries[0].Author, entries[1].Author)
	}
	if entries[2].Author != "missing-user" {
		t.Fatalf("missing author = %q, want raw UUID", entries[2].Author)
	}
	if lookup.calls["user-1"] != 1 {
		t.Fatalf("user-1 lookup count = %d, want 1", lookup.calls["user-1"])
	}
	if lookup.calls["missing-user"] != 1 {
		t.Fatalf("missing-user lookup count = %d, want 1", lookup.calls["missing-user"])
	}
}
