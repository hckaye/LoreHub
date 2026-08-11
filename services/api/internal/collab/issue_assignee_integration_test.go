package collab

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestIntegrationIssueAssigneesAndTenantBoundary(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "private", "triage")
	other := setupFixture(t, pool, "private", "read")
	issueID := uuidNew()
	mustExec(t, ctx, pool, `
		INSERT INTO issues (id, repository_id, number, title, body, author_id)
		VALUES ($1, $2, 1, 'Assign this issue', '', $3)
	`, issueID, fixture.repoID, fixture.alice.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
			DELETE FROM outbox_events
			WHERE topic = 'issue.assignees.updated' AND event_key LIKE $1
		`, issueID+"%")
	})

	candidates, err := store.ListAssignableUsers(ctx, fixture.repoID, "", Page{Limit: 20})
	if err != nil {
		t.Fatalf("list assignable users: %v", err)
	}
	if !hasAssignee(candidates.Items, fixture.alice.Username) ||
		!hasAssignee(candidates.Items, fixture.bob.Username) ||
		hasAssignee(candidates.Items, fixture.carol.Username) {
		t.Fatalf("assignable users = %#v", candidates.Items)
	}
	filtered, err := store.ListAssignableUsers(ctx, fixture.repoID, fixture.bob.Username, Page{Limit: 1})
	if err != nil || len(filtered.Items) != 1 || filtered.Items[0].ID != fixture.bob.ID {
		t.Fatalf("filtered assignable users = %#v, error = %v", filtered, err)
	}

	alice, applied, err := store.AssignIssueUser(
		ctx, fixture.bob, fixture.repoID, 1, fixture.alice.Username,
	)
	if err != nil || !applied || alice.ID != fixture.alice.ID {
		t.Fatalf("assign owner = %#v, applied = %t, error = %v", alice, applied, err)
	}
	if _, applied, err := store.AssignIssueUser(
		ctx, fixture.bob, fixture.repoID, 1, fixture.alice.Username,
	); err != nil || applied {
		t.Fatalf("idempotent assignment applied = %t, error = %v", applied, err)
	}
	if _, _, err := store.AssignIssueUser(
		ctx, fixture.bob, fixture.repoID, 1, fixture.carol.Username,
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("ineligible assignee error = %v, want not found", err)
	}
	if _, applied, err := store.AssignIssueUser(
		ctx, fixture.bob, fixture.repoID, 1, fixture.bob.Username,
	); err != nil || !applied {
		t.Fatalf("assign triager applied = %t, error = %v", applied, err)
	}

	issue, err := store.GetIssue(ctx, fixture.repoID, 1)
	if err != nil || len(issue.Assignees) != 2 ||
		!hasAssignee(issue.Assignees, fixture.alice.Username) ||
		!hasAssignee(issue.Assignees, fixture.bob.Username) {
		t.Fatalf("issue assignees = %#v, error = %v", issue.Assignees, err)
	}
	if issue.Assignee == nil || *issue.Assignee != issue.Assignees[0].Username {
		t.Fatalf("primary assignee = %v, assignees = %#v", issue.Assignee, issue.Assignees)
	}
	assertAssigneeRepositoryBoundary(t, pool, issueID, other.repoID, fixture.carol.ID)

	mustExec(t, ctx, pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, fixture.alice.ID)
	if err := store.RemoveIssueUser(
		ctx, fixture.bob, fixture.repoID, 1, fixture.alice.Username,
	); err != nil {
		t.Fatalf("remove suspended assignee: %v", err)
	}
	mustExec(t, ctx, pool, `UPDATE users SET status = 'active' WHERE id = $1`, fixture.alice.ID)
	mustExec(t, ctx, pool, `
		UPDATE repository_memberships SET active = false
		WHERE repository_id = $1 AND user_id = $2
	`, fixture.repoID, fixture.bob.ID)
	if err := store.RemoveIssueUser(
		ctx, fixture.bob, fixture.repoID, 1, fixture.bob.Username,
	); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("inactive actor error = %v, want forbidden", err)
	}
	mustExec(t, ctx, pool, `
		UPDATE repository_memberships SET active = true
		WHERE repository_id = $1 AND user_id = $2
	`, fixture.repoID, fixture.bob.ID)
	if err := store.RemoveIssueUser(
		ctx, fixture.bob, fixture.repoID, 1, fixture.bob.Username,
	); err != nil {
		t.Fatalf("remove assignee: %v", err)
	}

	assertAssigneeAuditAndOutbox(t, pool, fixture.repoID, issueID)
	var legacyColumnExists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'issues'
			  AND column_name = 'assignee_id'
		)
	`).Scan(&legacyColumnExists); err != nil || legacyColumnExists {
		t.Fatalf("legacy assignee column exists = %t, error = %v", legacyColumnExists, err)
	}
}

func TestIntegrationIssueAssigneeLimit(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "private", "")
	issueID := uuidNew()
	mustExec(t, ctx, pool, `
		INSERT INTO issues (id, repository_id, number, title, body, author_id)
		VALUES ($1, $2, 1, 'Limit assignees', '', $3)
	`, issueID, fixture.repoID, fixture.alice.ID)
	users := make([]platform.User, 0, maxIssueAssignees+1)
	userIDs := make([]string, 0, maxIssueAssignees+1)
	for index := 0; index <= maxIssueAssignees; index++ {
		user := platform.User{
			ID:          uuidNew(),
			Username:    fmt.Sprintf("assignee-%02d-%s", index, fixture.orgID[:8]),
			DisplayName: fmt.Sprintf("Assignee %02d", index),
		}
		mustExec(t, ctx, pool, `
			INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
		`, user.ID, user.Username, user.DisplayName)
		mustExec(t, ctx, pool, `
			INSERT INTO repository_memberships (repository_id, user_id, role)
			VALUES ($1, $2, 'read')
		`, fixture.repoID, user.ID)
		users = append(users, user)
		userIDs = append(userIDs, user.ID)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
			DELETE FROM outbox_events
			WHERE topic = 'issue.assignees.updated' AND event_key LIKE $1
		`, issueID+"%")
		_, _ = pool.Exec(context.Background(), `DELETE FROM issues WHERE id = $1`, issueID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1::uuid[])`, userIDs)
	})

	for index := 0; index < maxIssueAssignees; index++ {
		if _, applied, err := store.AssignIssueUser(
			ctx, fixture.alice, fixture.repoID, 1, users[index].Username,
		); err != nil || !applied {
			t.Fatalf("assign user %d applied = %t, error = %v", index, applied, err)
		}
	}
	if _, _, err := store.AssignIssueUser(
		ctx, fixture.alice, fixture.repoID, 1, users[maxIssueAssignees].Username,
	); !errors.Is(err, ErrAssigneeLimit) {
		t.Fatalf("eleventh assignee error = %v, want limit", err)
	}
	issue, err := store.GetIssue(ctx, fixture.repoID, 1)
	if err != nil || len(issue.Assignees) != maxIssueAssignees {
		t.Fatalf("issue assignee count = %d, error = %v", len(issue.Assignees), err)
	}
}

func hasAssignee(assignees []Assignee, username string) bool {
	for _, assignee := range assignees {
		if assignee.Username == username {
			return true
		}
	}
	return false
}

func assertAssigneeRepositoryBoundary(
	t *testing.T,
	pool *pgxpool.Pool,
	issueID string,
	repositoryID string,
	userID string,
) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO issue_assignees (
			issue_id, repository_id, user_id, assigned_by
		) VALUES ($1, $2, $3, $3)
	`, issueID, repositoryID, userID)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
		t.Fatalf("cross-repository assignee error = %v, want foreign key violation", err)
	}
}

func assertAssigneeAuditAndOutbox(
	t *testing.T,
	pool *pgxpool.Pool,
	repositoryID string,
	issueID string,
) {
	t.Helper()
	var auditCount, outboxCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM audit_events
		WHERE repository_id = $1 AND target_id = $2
		  AND action LIKE 'issue.assignee.%'
	`, repositoryID, issueID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM outbox_events
		WHERE topic = 'issue.assignees.updated' AND event_key LIKE $1
	`, issueID+"%").Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 4 || outboxCount != 4 {
		t.Fatalf("assignee audit = %d, outbox = %d, want 4 each", auditCount, outboxCount)
	}
}
