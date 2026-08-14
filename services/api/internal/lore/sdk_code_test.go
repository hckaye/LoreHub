package lore

import (
	"testing"
	"time"

	"github.com/EpicGames/lore-go/types"
)

func TestApplyRevisionMetadata(t *testing.T) {
	timestamp := uint64(1786705601094)
	author := "user-uuid"
	message := "update README"
	result := Revision{}

	applyRevisionMetadata(&result, "timestamp", types.LoreMetadata{
		Tag: types.LoreMetadataTag_NUMERIC, Numeric: &timestamp,
	})
	applyRevisionMetadata(&result, "created-by", types.LoreMetadata{
		Tag: types.LoreMetadataTag_STRING, String: &author,
	})
	applyRevisionMetadata(&result, "message", types.LoreMetadata{
		Tag: types.LoreMetadataTag_STRING, String: &message,
	})

	if got, want := result.CreatedAt, time.UnixMilli(int64(timestamp)).UTC(); !got.Equal(want) {
		t.Fatalf("CreatedAt = %v, want %v", got, want)
	}
	if result.Author != author {
		t.Fatalf("Author = %q, want %q", result.Author, author)
	}
	if result.Message != message {
		t.Fatalf("Message = %q, want %q", result.Message, message)
	}
}

func TestApplyRevisionMetadataIgnoresUnexpectedValues(t *testing.T) {
	result := Revision{Author: "existing", Message: "existing", CreatedAt: time.Unix(1, 0)}
	value := "not numeric"
	applyRevisionMetadata(&result, "timestamp", types.LoreMetadata{
		Tag: types.LoreMetadataTag_STRING, String: &value,
	})
	applyRevisionMetadata(&result, "unknown", types.LoreMetadata{
		Tag: types.LoreMetadataTag_STRING, String: &value,
	})

	if result.Author != "existing" || result.Message != "existing" || !result.CreatedAt.Equal(time.Unix(1, 0)) {
		t.Fatalf("unexpected metadata changed revision: %+v", result)
	}
}
