package collab

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestIntegrationMergeRequestMetadata(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "private", "triage")
	other := setupFixture(t, pool, "private", "read")
	number := seedMergeRequest(t, ctx, pool, fixture, fixture.alice.ID, "metadata-revision")
	request, err := store.GetMergeRequest(ctx, fixture.repoID, number)
	if err != nil {
		t.Fatal(err)
	}
	label, err := store.CreateLabel(
		ctx, fixture.alice, fixture.repoID, LabelInput{Name: "reviewed", Color: "1f6feb"},
	)
	if err != nil {
		t.Fatal(err)
	}
	milestone := seedMergeRequestMilestone(t, ctx, pool, fixture)

	metadata, err := store.GetMergeRequestMetadata(ctx, fixture.repoID, number)
	if err != nil || len(metadata.Labels) != 0 || len(metadata.Assignees) != 0 || metadata.Milestone != nil {
		t.Fatalf("initial metadata = %#v, error = %v", metadata, err)
	}
	if applied, created, err := store.ApplyMergeRequestLabel(
		ctx, fixture.bob, fixture.repoID, number, label.ID,
	); err != nil || !created || applied.ID != label.ID {
		t.Fatalf("apply label = %#v, created = %t, error = %v", applied, created, err)
	}
	if _, created, err := store.ApplyMergeRequestLabel(
		ctx, fixture.bob, fixture.repoID, number, label.ID,
	); err != nil || created {
		t.Fatalf("idempotent label created = %t, error = %v", created, err)
	}
	if assignee, created, err := store.AssignMergeRequestUser(
		ctx, fixture.bob, fixture.repoID, number, fixture.alice.Username,
	); err != nil || !created || assignee.ID != fixture.alice.ID {
		t.Fatalf("assign user = %#v, created = %t, error = %v", assignee, created, err)
	}
	if _, created, err := store.AssignMergeRequestUser(
		ctx, fixture.bob, fixture.repoID, number, fixture.alice.Username,
	); err != nil || created {
		t.Fatalf("idempotent assignee created = %t, error = %v", created, err)
	}
	assignedMilestone, changed, err := store.SetMergeRequestMilestone(
		ctx, fixture.bob, fixture.repoID, number, &milestone.Number,
	)
	if err != nil || !changed || assignedMilestone == nil || assignedMilestone.ID != milestone.ID {
		t.Fatalf("set milestone = %#v, changed = %t, error = %v", assignedMilestone, changed, err)
	}
	if _, changed, err := store.SetMergeRequestMilestone(
		ctx, fixture.bob, fixture.repoID, number, &milestone.Number,
	); err != nil || changed {
		t.Fatalf("idempotent milestone changed = %t, error = %v", changed, err)
	}

	metadata, err = store.GetMergeRequestMetadata(ctx, fixture.repoID, number)
	if err != nil || len(metadata.Labels) != 1 || len(metadata.Assignees) != 1 ||
		metadata.Milestone == nil || metadata.Milestone.ID != milestone.ID {
		t.Fatalf("assigned metadata = %#v, error = %v", metadata, err)
	}
	otherLabel, err := store.CreateLabel(
		ctx, other.alice, other.repoID, LabelInput{Name: "other", Color: "ffffff"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApplyMergeRequestLabel(
		ctx, fixture.bob, fixture.repoID, number, otherLabel.ID,
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("cross-repository label error = %v, want not found", err)
	}
	assertMergeRequestMetadataBoundary(t, pool, request.ID, other.repoID, otherLabel.ID, fixture.alice.ID)
	if _, _, err := store.AssignMergeRequestUser(
		ctx, fixture.carol, fixture.repoID, number, fixture.alice.Username,
	); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("reader metadata mutation error = %v, want forbidden", err)
	}

	mustExec(t, ctx, pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, fixture.alice.ID)
	if err := store.RemoveMergeRequestUser(
		ctx, fixture.bob, fixture.repoID, number, fixture.alice.Username,
	); err != nil {
		t.Fatalf("remove suspended assignee: %v", err)
	}
	mustExec(t, ctx, pool, `UPDATE users SET status = 'active' WHERE id = $1`, fixture.alice.ID)
	if err := store.RemoveMergeRequestLabel(
		ctx, fixture.bob, fixture.repoID, number, label.ID,
	); err != nil {
		t.Fatalf("remove label: %v", err)
	}
	if err := store.RemoveMergeRequestLabel(
		ctx, fixture.bob, fixture.repoID, number, label.ID,
	); err != nil {
		t.Fatalf("idempotent label removal: %v", err)
	}
	if removed, changed, err := store.SetMergeRequestMilestone(
		ctx, fixture.bob, fixture.repoID, number, nil,
	); err != nil || !changed || removed != nil {
		t.Fatalf("remove milestone = %#v, changed = %t, error = %v", removed, changed, err)
	}
	if _, changed, err := store.SetMergeRequestMilestone(
		ctx, fixture.bob, fixture.repoID, number, nil,
	); err != nil || changed {
		t.Fatalf("idempotent milestone removal changed = %t, error = %v", changed, err)
	}
	assertMergeRequestMetadataAudit(t, pool, fixture.repoID, request.ID)
}

func seedMergeRequestMilestone(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture integrationFixture,
) MilestoneSummary {
	t.Helper()
	milestone := MilestoneSummary{
		ID: uuidNew(), Number: 1, Title: "Version 1", State: "open",
	}
	mustExec(t, ctx, pool, `
		INSERT INTO repository_milestones (
			id, repository_id, number, title, created_by
		) VALUES ($1, $2, $3, $4, $5)
	`, milestone.ID, fixture.repoID, milestone.Number, milestone.Title, fixture.alice.ID)
	return milestone
}

func assertMergeRequestMetadataBoundary(
	t *testing.T,
	pool *pgxpool.Pool,
	mergeRequestID string,
	repositoryID string,
	labelID string,
	actorID string,
) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO merge_request_labels (
			merge_request_id, repository_id, label_id, applied_by
		) VALUES ($1, $2, $3, $4)
	`, mergeRequestID, repositoryID, labelID, actorID)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
		t.Fatalf("cross-repository metadata error = %v, want foreign key violation", err)
	}
}

func assertMergeRequestMetadataAudit(
	t *testing.T,
	pool *pgxpool.Pool,
	repositoryID string,
	mergeRequestID string,
) {
	t.Helper()
	var auditCount, outboxCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM audit_events
		WHERE repository_id = $1 AND target_id = $2
		  AND (
		    action LIKE 'merge_request.label.%'
		    OR action LIKE 'merge_request.assignee.%'
		    OR action LIKE 'merge_request.milestone.%'
		  )
	`, repositoryID, mergeRequestID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM outbox_events
		WHERE topic = 'merge_request.updated' AND event_key LIKE $1
	`, mergeRequestID+"%").Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 6 || outboxCount != 6 {
		t.Fatalf("metadata audit = %d, outbox = %d, want 6 each", auditCount, outboxCount)
	}
}
