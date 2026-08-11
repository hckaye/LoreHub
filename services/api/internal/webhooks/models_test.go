package webhooks

import (
	"reflect"
	"testing"
)

func TestWikiWebhookEventsAreSupported(t *testing.T) {
	events, err := normalizeEvents([]string{"wiki", "issues"})
	if err != nil {
		t.Fatalf("normalize wiki events: %v", err)
	}
	if !reflect.DeepEqual(events, []string{"issues", "wiki"}) {
		t.Fatalf("events = %v", events)
	}
	if kind, ok := eventKind("wiki.updated"); !ok || kind != "wiki" {
		t.Fatalf("wiki event kind = %q, supported = %t", kind, ok)
	}
}
