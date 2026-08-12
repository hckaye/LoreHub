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

func TestReviewThreadWebhookEventsUseReviewSubscriptions(t *testing.T) {
	for _, topic := range []string{
		"merge_request_review_request.created",
		"merge_request_review_thread.created",
		"merge_request_review_comment.created",
	} {
		if kind, ok := eventKind(topic); !ok || kind != "reviews" {
			t.Fatalf("review event %q kind = %q, supported = %t", topic, kind, ok)
		}
	}
}

func TestDiscussionWebhookEventsAreSupported(t *testing.T) {
	events, err := normalizeEvents([]string{"discussions", "issues"})
	if err != nil {
		t.Fatalf("normalize discussion events: %v", err)
	}
	if !reflect.DeepEqual(events, []string{"discussions", "issues"}) {
		t.Fatalf("events = %v", events)
	}
	if kind, ok := eventKind("discussion.comment.created"); !ok || kind != "discussions" {
		t.Fatalf("discussion event kind = %q, supported = %t", kind, ok)
	}
}
