package webhooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestDiscussionEventsProjectToSubscribedWebhooks(t *testing.T) {
	fixture := openWebhookFixture(t)
	ctx := context.Background()
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	webhook, err := fixture.store.Create(ctx, fixture.owner, fixture.orgSlug, "game", CreateInput{
		URL: target.URL, Events: []string{"discussions"}, Secret: "discussion-webhook-secret",
	})
	if err != nil {
		t.Fatalf("create discussion webhook: %v", err)
	}
	discussionID := uuid.NewString()
	mustWebhookExec(t, fixture.pool, `
		INSERT INTO discussions (id, repository_id, category_id, number, author_id, title, body)
		SELECT $1, $2, category.id, 1, $3, 'Build performance', 'How can builds be faster?'
		FROM discussion_categories category
		WHERE category.repository_id = $2 AND category.slug = 'questions'
	`, discussionID, fixture.repoID, fixture.owner.ID)
	eventID := uuid.NewString()
	mustWebhookExec(t, fixture.pool, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, 'discussion.created', $2, $3)
	`, eventID, discussionID+":"+uuid.NewString(), `{"repositoryId":"`+fixture.repoID+`"}`)

	var deliveryCount int
	for attempt := 0; attempt < 20 && deliveryCount == 0; attempt++ {
		if _, err := fixture.store.Project(ctx); err != nil {
			t.Fatalf("project discussion event: %v", err)
		}
		if err := fixture.pool.QueryRow(ctx, `
			SELECT count(*) FROM webhook_deliveries
			WHERE webhook_id = $1 AND source_event_id = $2
		`, webhook.ID, eventID).Scan(&deliveryCount); err != nil {
			t.Fatalf("count discussion deliveries: %v", err)
		}
	}
	if deliveryCount != 1 {
		t.Fatalf("discussion delivery count = %d", deliveryCount)
	}
	var eventName string
	var requestBody []byte
	if err := fixture.pool.QueryRow(ctx, `
		SELECT event_name, request_body FROM webhook_deliveries
		WHERE webhook_id = $1 AND source_event_id = $2
	`, webhook.ID, eventID).Scan(&eventName, &requestBody); err != nil {
		t.Fatalf("read projected discussion delivery: %v", err)
	}
	var envelope deliveryEnvelope
	if err := json.Unmarshal(requestBody, &envelope); err != nil {
		t.Fatalf("decode discussion delivery: %v", err)
	}
	if eventName != "discussion.created" || envelope.Event != "discussion.created" ||
		envelope.Repository.ID != fixture.repoID {
		t.Fatalf("discussion delivery = event %q envelope %+v", eventName, envelope)
	}
}
