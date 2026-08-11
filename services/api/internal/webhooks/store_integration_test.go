package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/database"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type webhookFixture struct {
	pool      *pgxpool.Pool
	store     *Store
	owner     platform.User
	teamAdmin platform.User
	reader    platform.User
	orgID     string
	repoID    string
	orgSlug   string
}

func TestWebhookManagementProjectionDeliveryAndTenantBoundaryIntegration(t *testing.T) {
	fixture := openWebhookFixture(t)
	ctx := context.Background()
	secret := "0123456789abcdef0123456789abcdef"
	var mutex sync.Mutex
	requests := make([]receivedWebhook, 0)
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
		}
		mutex.Lock()
		requests = append(requests, receivedWebhook{
			body: body, signature: request.Header.Get("X-LoreHub-Signature-256"),
			event: request.Header.Get("X-LoreHub-Event"),
		})
		mutex.Unlock()
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	resolver := &countingResolver{}
	originalResolver := fixture.store.target.resolver
	fixture.store.target.resolver = resolver
	_, err := fixture.store.Create(ctx, fixture.reader, fixture.orgSlug, "game", CreateInput{
		URL: "https://lookup.example/hook", Events: []string{"issues"}, Secret: secret,
	})
	fixture.store.target.resolver = originalResolver
	if !errors.Is(err, ErrForbidden) || resolver.calls != 0 {
		t.Fatalf("unauthorized create error = %v DNS calls = %d", err, resolver.calls)
	}

	webhook, err := fixture.store.Create(ctx, fixture.teamAdmin, fixture.orgSlug, "game", CreateInput{
		URL: target.URL, Events: []string{"issues", "branches"}, Secret: secret,
	})
	if err != nil {
		t.Fatalf("create webhook as team administrator: %v", err)
	}
	if webhook.URL != target.URL || !webhook.SecretConfigured || len(webhook.Events) != 2 {
		t.Fatalf("created webhook = %#v", webhook)
	}
	if _, err := fixture.store.List(ctx, fixture.reader, fixture.orgSlug, "game"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reader list error = %v", err)
	}
	items, err := fixture.store.List(ctx, fixture.owner, fixture.orgSlug, "game")
	if err != nil || len(items) != 1 || !items[0].SecretConfigured {
		t.Fatalf("owner webhook list = %#v error = %v", items, err)
	}
	assertEncryptedWebhookSecret(t, fixture.pool, webhook.ID, secret)

	eventID := uuid.NewString()
	mustWebhookExec(t, fixture.pool, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, 'branch.pushed', $2, $3)
	`, eventID, "branch-id:"+uuid.NewString(), `{"repositoryId":"`+fixture.repoID+`",`+
		`"branchName":"main","latestRevision":"revision-1"}`)
	projectConcurrently(t, fixture.store, fixture.pool, eventID)
	worker, err := NewWorker(fixture.store, time.Second, 5*time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	secondWorker, err := NewWorker(fixture.store, time.Second, 5*time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	runWorkersConcurrently(t, ctx, worker, secondWorker)
	assertReceivedWebhook(t, &mutex, requests, secret)

	deliveries, err := fixture.store.ListDeliveries(
		ctx, fixture.owner, fixture.orgSlug, "game", webhook.ID, 30,
	)
	if err != nil || len(deliveries) != 1 || deliveries[0].Status != "succeeded" ||
		deliveries[0].AttemptCount != 1 || deliveries[0].ResponseStatus == nil ||
		*deliveries[0].ResponseStatus != http.StatusNoContent {
		t.Fatalf("deliveries = %#v error = %v", deliveries, err)
	}
	detail, err := fixture.store.DeliveryDetail(
		ctx, fixture.owner, fixture.orgSlug, "game", webhook.ID, deliveries[0].ID,
	)
	if err != nil || len(detail.Attempts) != 1 || detail.Attempts[0].AttemptNumber != 1 {
		t.Fatalf("delivery detail = %#v error = %v", detail, err)
	}
	if err := fixture.store.Redeliver(
		ctx, fixture.owner, fixture.orgSlug, "game", webhook.ID, deliveries[0].ID,
	); err != nil {
		t.Fatal(err)
	}
	worker.runCycle(ctx)
	mutex.Lock()
	requestCount := len(requests)
	mutex.Unlock()
	if requestCount != 2 {
		t.Fatalf("request count after redelivery = %d", requestCount)
	}

	active := false
	if _, err := fixture.store.Update(
		ctx, fixture.owner, fixture.orgSlug, "game", webhook.ID, UpdateInput{Active: &active},
	); err != nil {
		t.Fatal(err)
	}
	secondEventID := uuid.NewString()
	mustWebhookExec(t, fixture.pool, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, 'branch.pushed', $2, $3)
	`, secondEventID, "branch-id:"+uuid.NewString(), `{"repositoryId":"`+fixture.repoID+`"}`)
	if _, err := fixture.store.Project(ctx); err != nil {
		t.Fatal(err)
	}
	var secondDeliveries int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT count(*) FROM webhook_deliveries WHERE source_event_id = $1
	`, secondEventID).Scan(&secondDeliveries); err != nil || secondDeliveries != 0 {
		t.Fatalf("inactive webhook deliveries = %d error = %v", secondDeliveries, err)
	}

	mustWebhookExec(t, fixture.pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, fixture.teamAdmin.ID)
	if _, err := fixture.store.List(ctx, fixture.teamAdmin, fixture.orgSlug, "game"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("suspended administrator error = %v", err)
	}
	var auditCount int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_events
		WHERE repository_id = $1 AND action IN ('webhook.create', 'webhook.update', 'webhook.redeliver')
	`, fixture.repoID).Scan(&auditCount); err != nil || auditCount != 3 {
		t.Fatalf("webhook audit count = %d error = %v", auditCount, err)
	}
}

type receivedWebhook struct {
	body      []byte
	signature string
	event     string
}

type countingResolver struct {
	calls int
}

func (resolver *countingResolver) LookupNetIP(
	context.Context,
	string,
	string,
) ([]netip.Addr, error) {
	resolver.calls++
	return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
}

func assertReceivedWebhook(
	t *testing.T,
	mutex *sync.Mutex,
	requests []receivedWebhook,
	secret string,
) {
	t.Helper()
	mutex.Lock()
	defer mutex.Unlock()
	if len(requests) != 1 || requests[0].event != "branch.pushed" {
		t.Fatalf("received requests = %#v", requests)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(requests[0].body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if requests[0].signature != want {
		t.Fatalf("signature = %q want %q", requests[0].signature, want)
	}
	var envelope deliveryEnvelope
	if err := json.Unmarshal(requests[0].body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Repository.Name != "game" || bytes.Contains(requests[0].body, []byte(secret)) {
		t.Fatalf("delivery envelope = %#v", envelope)
	}
}

func assertEncryptedWebhookSecret(t *testing.T, pool *pgxpool.Pool, webhookID string, secret string) {
	t.Helper()
	var ciphertext []byte
	var nonce []byte
	if err := pool.QueryRow(context.Background(), `
		SELECT secret_ciphertext, secret_nonce FROM repository_webhooks WHERE id = $1
	`, webhookID).Scan(&ciphertext, &nonce); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ciphertext, []byte(secret)) || bytes.Contains(ciphertext, []byte(secret)) || len(nonce) != 12 {
		t.Fatal("webhook secret was not stored as authenticated ciphertext")
	}
}

func projectConcurrently(t *testing.T, store *Store, pool *pgxpool.Pool, eventID string) {
	t.Helper()
	errorsFound := make(chan error, 8)
	var workers sync.WaitGroup
	for index := 0; index < 8; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := store.Project(context.Background())
			errorsFound <- err
		}()
	}
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM webhook_deliveries WHERE source_event_id = $1
	`, eventID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("concurrent projection delivery count = %d error = %v", count, err)
	}
}

func runWorkersConcurrently(t *testing.T, ctx context.Context, workers ...*Worker) {
	t.Helper()
	var group sync.WaitGroup
	for _, worker := range workers {
		group.Add(1)
		go func(current *Worker) {
			defer group.Done()
			current.runCycle(ctx)
		}(worker)
	}
	group.Wait()
}

func openWebhookFixture(t *testing.T) webhookFixture {
	t.Helper()
	databaseURL := os.Getenv("LOREHUB_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("LOREHUB_TEST_DATABASE_URL or DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseURL, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	box, err := NewSecretBox("test-webhooks-v1", testEncodedKey)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewTargetPolicy(true, true, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(pool, box, target)
	if err != nil {
		t.Fatal(err)
	}
	fixture := seedWebhookFixture(t, pool, store)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
			DELETE FROM outbox_events WHERE payload->>'repositoryId' = $1
		`, fixture.repoID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, fixture.orgID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1, $2, $3)`,
			fixture.owner.ID, fixture.teamAdmin.ID, fixture.reader.ID)
	})
	return fixture
}

func seedWebhookFixture(t *testing.T, pool *pgxpool.Pool, store *Store) webhookFixture {
	t.Helper()
	identifier := strings.ReplaceAll(uuid.NewString(), "-", "")
	owner := platform.User{ID: uuid.NewString(), Username: "hook-owner-" + identifier[:8]}
	admin := platform.User{ID: uuid.NewString(), Username: "hook-admin-" + identifier[:8]}
	reader := platform.User{ID: uuid.NewString(), Username: "hook-reader-" + identifier[:8]}
	orgID, repoID, teamID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	orgSlug := "hook-org-" + identifier[:8]
	mustWebhookExec(t, pool, `
		INSERT INTO users (id, username, display_name) VALUES
		($1, $2, 'Owner'), ($3, $4, 'Admin'), ($5, $6, 'Reader')
	`, owner.ID, owner.Username, admin.ID, admin.Username, reader.ID, reader.Username)
	mustWebhookExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Webhook Org', 'private', $3)
	`, orgID, orgSlug, owner.ID)
	mustWebhookExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role) VALUES
		($1, $2, 'owner'), ($1, $3, 'member'), ($1, $4, 'member')
	`, orgID, owner.ID, admin.ID, reader.ID)
	mustWebhookExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, 'game', 'Game', 'private', $3, $4, 'main', $5)
	`, repoID, orgID, identifier, "lores://lore.example/"+identifier, owner.ID)
	mustWebhookExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role)
		VALUES ($1, $2, 'read')
	`, repoID, reader.ID)
	mustWebhookExec(t, pool, `
		INSERT INTO teams (id, organization_id, slug, display_name, created_by)
		VALUES ($1, $2, 'operators', 'Operators', $3)
	`, teamID, orgID, owner.ID)
	mustWebhookExec(t, pool, `
		INSERT INTO team_memberships (team_id, user_id, role) VALUES ($1, $2, 'member')
	`, teamID, admin.ID)
	mustWebhookExec(t, pool, `
		INSERT INTO team_repository_roles (team_id, repository_id, role, created_by)
		VALUES ($1, $2, 'admin', $3)
	`, teamID, repoID, owner.ID)
	return webhookFixture{
		pool: pool, store: store, owner: owner, teamAdmin: admin, reader: reader,
		orgID: orgID, repoID: repoID, orgSlug: orgSlug,
	}
}

func mustWebhookExec(t *testing.T, pool *pgxpool.Pool, query string, arguments ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), query, arguments...); err != nil {
		t.Fatal(err)
	}
}
