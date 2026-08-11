package lore

import (
	"errors"
	"testing"

	loresdk "github.com/EpicGames/lore-go"
)

func TestIsEmptyRemoteCloneError(t *testing.T) {
	t.Parallel()
	if !isEmptyRemoteCloneError(&loresdk.LoreError{ReturnCode: 13, Messages: []string{"Not found"}}) {
		t.Fatal("expected Lore's empty remote clone error to be recognized")
	}
	for _, err := range []error{
		errors.New("Not found"),
		&loresdk.LoreError{ReturnCode: 7, Messages: []string{"Not found"}},
		&loresdk.LoreError{ReturnCode: 13, Messages: []string{"Not authorized"}},
	} {
		if isEmptyRemoteCloneError(err) {
			t.Fatalf("unexpected empty remote clone error: %v", err)
		}
	}
}
