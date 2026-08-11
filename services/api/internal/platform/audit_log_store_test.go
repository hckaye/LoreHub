package platform

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuditCursorRoundTripAndValidation(t *testing.T) {
	t.Parallel()
	event := AuditEvent{
		ID:         uuid.NewString(),
		OccurredAt: time.Date(2026, time.August, 12, 4, 5, 6, 789, time.FixedZone("test", 9*60*60)),
	}
	encoded := encodeAuditCursor(event)
	decoded, err := decodeAuditCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID.String() != event.ID || !decoded.OccurredAt.Equal(event.OccurredAt) {
		t.Fatalf("decoded cursor = %+v, want id=%s time=%s", decoded, event.ID, event.OccurredAt)
	}
	for _, value := range []string{"not-base64!", "bm8tc2VwYXJhdG9y", "MjAyNi0wMS0wMXxiYWQtaWQ"} {
		if _, err := decodeAuditCursor(value); !errors.Is(err, ErrInvalidAuditCursor) {
			t.Fatalf("decodeAuditCursor(%q) error = %v, want invalid cursor", value, err)
		}
	}
	if decoded, err := decodeAuditCursor(""); err != nil || decoded != nil {
		t.Fatalf("empty cursor = %+v, %v", decoded, err)
	}
}

func TestAuditDetailsRedactSecretsRecursively(t *testing.T) {
	t.Parallel()
	details := redactAuditDetails(map[string]any{
		"name":  "DEPLOY_TOKEN",
		"value": "plain text",
		"nested": map[string]any{
			"token":  "bearer token",
			"key_id": "current-key",
		},
	})
	if details["value"] != "[REDACTED]" {
		t.Fatalf("value detail = %q, want redacted", details["value"])
	}
	nested := details["nested"].(map[string]any)
	if nested["token"] != "[REDACTED]" || nested["key_id"] != "current-key" {
		t.Fatalf("nested details = %+v", nested)
	}
}
