package webhooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestRevisionStatusEventsProjectToSubscribedWebhooks(t *testing.T) {
	fixture := openWebhookFixture(t)
	ctx := context.Background()
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	webhook, err := fixture.store.Create(ctx, fixture.owner, fixture.orgSlug, "game", CreateInput{
		URL: target.URL, Events: []string{"statuses"}, Secret: "revision-status-webhook-secret",
	})
	if err != nil {
		t.Fatalf("create status webhook: %v", err)
	}
	statusID := uuid.NewString()
	mustWebhookExec(t, fixture.pool, `
		INSERT INTO revision_statuses (
			id, repository_id, revision, context, state, description, creator_id
		) VALUES ($1, $2, repeat('a', 64), 'build/linux', 'success', 'Build passed', $3)
	`, statusID, fixture.repoID, fixture.owner.ID)
	eventID := uuid.NewString()
	mustWebhookExec(t, fixture.pool, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, 'revision_status.created', $2, $3)
	`, eventID, statusID, `{"repositoryId":"`+fixture.repoID+`","state":"success"}`)

	var deliveryCount int
	for attempt := 0; attempt < 20 && deliveryCount == 0; attempt++ {
		if _, err := fixture.store.Project(ctx); err != nil {
			t.Fatalf("project revision status event: %v", err)
		}
		if err := fixture.pool.QueryRow(ctx, `
			SELECT count(*) FROM webhook_deliveries
			WHERE webhook_id = $1 AND source_event_id = $2
		`, webhook.ID, eventID).Scan(&deliveryCount); err != nil {
			t.Fatalf("count revision status deliveries: %v", err)
		}
	}
	if deliveryCount != 1 {
		t.Fatalf("revision status delivery count = %d", deliveryCount)
	}
	var eventName string
	var requestBody []byte
	if err := fixture.pool.QueryRow(ctx, `
		SELECT event_name, request_body FROM webhook_deliveries
		WHERE webhook_id = $1 AND source_event_id = $2
	`, webhook.ID, eventID).Scan(&eventName, &requestBody); err != nil {
		t.Fatalf("read projected revision status delivery: %v", err)
	}
	var envelope deliveryEnvelope
	if err := json.Unmarshal(requestBody, &envelope); err != nil {
		t.Fatalf("decode revision status delivery: %v", err)
	}
	if eventName != "revision_status.created" || envelope.Event != "revision_status.created" ||
		envelope.Repository.ID != fixture.repoID {
		t.Fatalf("revision status delivery = event %q envelope %+v", eventName, envelope)
	}
}
