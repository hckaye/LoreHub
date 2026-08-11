package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOrganizationAuditLogAuthorizationFilteringAndPaginationIntegration(t *testing.T) {
	fixture := authorizationIntegrationFixture(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = fixture.pool.Exec(context.Background(), `DELETE FROM audit_events WHERE organization_id = $1`,
			fixture.orgID)
	})
	baseTime := time.Now().UTC().Add(-time.Hour)
	events := []struct {
		id           string
		action       string
		actorID      string
		repositoryID string
		targetID     string
		occurredAt   time.Time
	}{
		{uuid.NewString(), "audit.integration.first", fixture.alice.ID, fixture.repositoryA, "target-1",
			baseTime.Add(3 * time.Minute)},
		{uuid.NewString(), "audit.integration.second", fixture.manager.ID, "", "target-2",
			baseTime.Add(2 * time.Minute)},
		{uuid.NewString(), "audit.integration.third", fixture.bob.ID, fixture.repositoryB, "target-3",
			baseTime.Add(time.Minute)},
	}
	for _, event := range events {
		authorizationMustExec(t, fixture.pool, `
			INSERT INTO audit_events (
				id, organization_id, repository_id, actor_id, action,
				target_type, target_id, details, occurred_at
			) VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, 'integration', $6, $7, $8)
		`, event.id, fixture.orgID, event.repositoryID, event.actorID, event.action,
			event.targetID, `{"safe":"value"}`, event.occurredAt)
	}

	first, err := fixture.store.OrganizationAuditLog(ctx, fixture.manager, fixture.orgSlug, "", "", 2)
	if err != nil {
		t.Fatalf("list first audit page: %v", err)
	}
	if len(first.Items) != 2 || first.NextCursor == nil {
		t.Fatalf("first audit page = %+v, want two items and cursor", first)
	}
	if first.Items[0].Action != events[0].action || first.Items[0].Actor == nil ||
		first.Items[0].Repository == nil || first.Items[0].Details["safe"] != "value" {
		t.Fatalf("first audit item lost context: %+v", first.Items[0])
	}
	second, err := fixture.store.OrganizationAuditLog(
		ctx, fixture.manager, fixture.orgSlug, "", *first.NextCursor, 2,
	)
	if err != nil {
		t.Fatalf("list second audit page: %v", err)
	}
	if len(second.Items) != 1 || second.NextCursor != nil || second.Items[0].Action != events[2].action {
		t.Fatalf("second audit page = %+v, want final event", second)
	}

	filtered, err := fixture.store.OrganizationAuditLog(
		ctx, fixture.manager, fixture.orgSlug, "integration.second", "", 50,
	)
	if err != nil || len(filtered.Items) != 1 || filtered.Items[0].Action != events[1].action {
		t.Fatalf("filtered audit page = %+v, err=%v", filtered, err)
	}
	if _, err := fixture.store.OrganizationAuditLog(
		ctx, fixture.manager, fixture.orgSlug, "", "invalid", 50,
	); !errors.Is(err, ErrInvalidAuditCursor) {
		t.Fatalf("invalid cursor error = %v", err)
	}

	authorizationMustExec(t, fixture.pool, `
		UPDATE organization_memberships SET role = 'maintainer'
		WHERE organization_id = $1 AND user_id = $2
	`, fixture.orgID, fixture.alice.ID)
	if _, err := fixture.store.OrganizationAuditLog(
		ctx, fixture.alice, fixture.orgSlug, "", "", 50,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("maintainer audit access error = %v, want forbidden", err)
	}
	authorizationMustExec(t, fixture.pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, fixture.manager.ID)
	if _, err := fixture.store.OrganizationAuditLog(
		ctx, fixture.manager, fixture.orgSlug, "", "", 50,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("suspended owner audit access error = %v, want forbidden", err)
	}
	authorizationMustExec(t, fixture.pool, `UPDATE users SET status = 'active' WHERE id = $1`, fixture.manager.ID)

	removedActorID := uuid.NewString()
	removedUsername := "removed-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	removedRepositoryID := uuid.NewString()
	removedRepositorySlug := "removed-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO users (id, username, display_name) VALUES ($1, $2, 'Removed actor')
	`, removedActorID, removedUsername)
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Removed repository', 'private', $4, $5, 'main', $6)
	`, removedRepositoryID, fixture.orgID, removedRepositorySlug,
		strings.ReplaceAll(uuid.NewString(), "-", ""), "lore://"+removedRepositorySlug, fixture.manager.ID)
	authorizationMustExec(t, fixture.pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`,
		removedRepositoryID)
	removedEventID := uuid.NewString()
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO audit_events (
			id, organization_id, repository_id, actor_id, action, target_type, target_id
		) VALUES ($1, $2, $3, $4, 'audit.integration.deleted_context', 'integration', 'removed')
	`, removedEventID, fixture.orgID, removedRepositoryID, removedActorID)
	authorizationMustExec(t, fixture.pool, `DELETE FROM repositories WHERE id = $1`, removedRepositoryID)
	authorizationMustExec(t, fixture.pool, `DELETE FROM users WHERE id = $1`, removedActorID)
	preserved, err := fixture.store.OrganizationAuditLog(
		ctx, fixture.manager, fixture.orgSlug, "deleted_context", "", 50,
	)
	if err != nil || len(preserved.Items) != 1 {
		t.Fatalf("preserved audit context = %+v, err=%v", preserved, err)
	}
	preservedEvent := preserved.Items[0]
	if preservedEvent.Actor == nil || preservedEvent.Actor.ID != "" ||
		preservedEvent.Actor.Username != removedUsername {
		t.Fatalf("preserved actor = %+v", preservedEvent.Actor)
	}
	if preservedEvent.Repository == nil || preservedEvent.Repository.ID != "" ||
		preservedEvent.Repository.Owner != fixture.orgSlug ||
		preservedEvent.Repository.Slug != removedRepositorySlug {
		t.Fatalf("preserved repository = %+v", preservedEvent.Repository)
	}
}
