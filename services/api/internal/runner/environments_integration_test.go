package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/database"
)

func TestActionsEnvironmentProtectionPostgres(t *testing.T) {
	databaseURL := os.Getenv("LOREHUB_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LOREHUB_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := database.Open(ctx, databaseURL, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	fixture := newActionsFixture(t, pool)
	defer fixture.cleanup(t)
	reviewerID := uuid.NewString()
	reviewerUsername := "deployment-reviewer-" + strings.ToLower(uuid.NewString()[:8])
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, display_name) VALUES ($1, $2, 'Deployment Reviewer')
	`, reviewerID, reviewerUsername); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, fixture.organizationID, reviewerID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO repository_memberships (repository_id, user_id, role)
		VALUES ($1, $2, 'read')
	`, fixture.repositoryID, reviewerID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", reviewerID); err != nil {
			t.Error(err)
		}
	}()
	store := NewStore(pool)
	access, err := store.RepositoryForActions(ctx, fixture.owner, fixture.repositorySlug, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := store.UpsertEnvironment(ctx, access, fixture.userID, "Production", EnvironmentInput{
		PreventSelfReview: true,
		ReviewerUsernames: []string{reviewerUsername},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(environment.Reviewers) != 1 || environment.Reviewers[0].UserID != reviewerID {
		t.Fatalf("environment reviewers were not retained: %#v", environment)
	}
	repository := Repository{
		ID: fixture.repositoryID, Owner: fixture.owner, Slug: fixture.repositorySlug,
		LoreURL: "lore://fixture/repository", DefaultBranch: "main",
	}
	workflow := protectedWorkflowDefinition()
	if queued, err := store.ObserveBranch(ctx, repository, ObservedBranch{
		ID: "environment-main", Name: "main", LatestRevision: "environment-rev-1",
	}, workflow); err != nil || queued {
		t.Fatalf("initial protected workflow observation = %t, %v", queued, err)
	}
	if queued, err := store.ObserveBranch(ctx, repository, ObservedBranch{
		ID: "environment-main", Name: "main", LatestRevision: "environment-rev-2",
	}, workflow); err != nil || !queued {
		t.Fatalf("protected workflow update = %t, %v", queued, err)
	}
	if job, err := store.ClaimJob(ctx, "environment-worker", time.Minute); err != nil || job != nil {
		t.Fatalf("pending deployment was claimable: %#v, %v", job, err)
	}
	deployments, err := store.ListDeployments(ctx, access, reviewerID, 10)
	if err != nil || len(deployments) != 1 || !deployments[0].CanReview || deployments[0].Status != "pending" {
		t.Fatalf("pending deployment was not visible to its reviewer: %#v, %v", deployments, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE repository_memberships SET active = false
		WHERE repository_id = $1 AND user_id = $2
	`, fixture.repositoryID, reviewerID); err != nil {
		t.Fatal(err)
	}
	deployments, err = store.ListDeployments(ctx, access, reviewerID, 10)
	if err != nil || len(deployments) != 1 || deployments[0].CanReview {
		t.Fatalf("review was offered after repository access was revoked: %#v, %v", deployments, err)
	}
	if _, err := store.ReviewDeployment(
		ctx,
		access,
		reviewerID,
		deployments[0].ID,
		true,
	); !errors.Is(err, ErrActionForbidden) {
		t.Fatalf("review was accepted after repository access was revoked: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE repository_memberships SET active = true
		WHERE repository_id = $1 AND user_id = $2
	`, fixture.repositoryID, reviewerID); err != nil {
		t.Fatal(err)
	}
	approved, err := store.ReviewDeployment(ctx, access, reviewerID, deployments[0].ID, true)
	if err != nil || approved.Status != "queued" || approved.CanReview {
		t.Fatalf("deployment approval failed: %#v, %v", approved, err)
	}
	job, err := store.ClaimJob(ctx, "environment-worker", time.Minute)
	if err != nil || job == nil || job.Environment != "Production" {
		t.Fatalf("approved deployment was not claimable: %#v, %v", job, err)
	}
	if err := store.CompleteJob(ctx, *job, "environment-worker", "success", "", nil); err != nil {
		t.Fatal(err)
	}
	deployments, err = store.ListDeployments(ctx, access, reviewerID, 10)
	if err != nil || deployments[0].Status != "success" || deployments[0].CompletedAt == nil {
		t.Fatalf("deployment completion was not recorded: %#v, %v", deployments, err)
	}
	if _, err := store.UpsertEnvironment(ctx, access, fixture.userID, "Production", EnvironmentInput{
		WaitTimerMinutes: 1, PreventSelfReview: true,
	}); err != nil {
		t.Fatal(err)
	}
	if queued, err := store.ObserveBranch(ctx, repository, ObservedBranch{
		ID: "environment-main", Name: "main", LatestRevision: "environment-rev-3",
	}, workflow); err != nil || !queued {
		t.Fatalf("wait-timer workflow update = %t, %v", queued, err)
	}
	if job, err := store.ClaimJob(ctx, "early-worker", time.Minute); err != nil || job != nil {
		t.Fatalf("deployment bypassed its wait timer: %#v, %v", job, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE deployments SET wait_until = now() - interval '1 second'
		WHERE repository_id = $1 AND status = 'waiting'
	`, fixture.repositoryID); err != nil {
		t.Fatal(err)
	}
	waitedJob, err := store.ClaimJob(ctx, "waited-worker", time.Minute)
	if err != nil || waitedJob == nil {
		t.Fatalf("expired wait timer did not release deployment: %#v, %v", waitedJob, err)
	}
	if err := store.CompleteJob(ctx, *waitedJob, "waited-worker", "failure", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertEnvironment(ctx, access, fixture.userID, "Production", EnvironmentInput{
		PreventSelfReview: true, ReviewerUsernames: []string{environmentOwnerUsername(t, pool, fixture.userID)},
	}); err != nil {
		t.Fatal(err)
	}
	var workflowID string
	if err := pool.QueryRow(ctx, `
		SELECT id FROM ci_workflows WHERE repository_id = $1 AND path = $2
	`, fixture.repositoryID, workflow.Path).Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	selfRunID, err := store.enqueueRun(
		ctx, tx, repository, workflowID, nil, workflow.Name, workflow.Path, "workflow_dispatch",
		"main", "environment-rev-3", json.RawMessage(`{}`), "Production", fixture.userID, nil,
	)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	selfRun, err := store.actionRunByID(ctx, selfRunID.String())
	if err != nil {
		t.Fatal(err)
	}
	deployments, err = store.ListDeployments(ctx, access, fixture.userID, 10)
	if err != nil || len(deployments) == 0 || deployments[0].CanReview {
		t.Fatalf("self review was offered: %#v, %v", deployments, err)
	}
	if _, err := store.ReviewDeployment(
		ctx, access, fixture.userID, deployments[0].ID, true,
	); !errors.Is(err, ErrActionForbidden) {
		t.Fatalf("self review was accepted: %v", err)
	}
	if _, err := store.CancelActionRun(ctx, access, selfRun.RunNumber, fixture.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertEnvironment(ctx, access, fixture.userID, "Production", EnvironmentInput{
		PreventSelfReview: true, ReviewerUsernames: []string{reviewerUsername},
	}); err != nil {
		t.Fatal(err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rejectedRunID, err := store.enqueueRun(
		ctx, tx, repository, workflowID, nil, workflow.Name, workflow.Path, "workflow_dispatch",
		"main", "environment-rev-3", json.RawMessage(`{}`), "Production", fixture.userID, nil,
	)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	deployments, err = store.ListDeployments(ctx, access, reviewerID, 10)
	if err != nil || len(deployments) == 0 || !deployments[0].CanReview {
		t.Fatalf("rejectable deployment was not visible: %#v, %v", deployments, err)
	}
	rejected, err := store.ReviewDeployment(ctx, access, reviewerID, deployments[0].ID, false)
	if err != nil || rejected.Status != "rejected" {
		t.Fatalf("deployment rejection failed: %#v, %v", rejected, err)
	}
	rejectedRun, err := store.actionRunByID(ctx, rejectedRunID.String())
	if err != nil || rejectedRun.Status != "completed" || rejectedRun.Conclusion == nil ||
		*rejectedRun.Conclusion != "failure" {
		t.Fatalf("rejected deployment did not fail its run: %#v, %v", rejectedRun, err)
	}
	if err := store.DeleteEnvironment(ctx, access, fixture.userID, "Production"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListEnvironments(ctx, access, reviewerID); !errors.Is(err, ErrActionForbidden) {
		t.Fatalf("organization member administered environments: %v", err)
	}
}

func environmentOwnerUsername(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	var username string
	if err := pool.QueryRow(context.Background(), "SELECT username FROM users WHERE id = $1", userID).
		Scan(&username); err != nil {
		t.Fatal(err)
	}
	return username
}

func protectedWorkflowDefinition() WorkflowDefinition {
	triggerConfig, _ := json.Marshal(map[string]any{
		"push":        map[string]any{"branches": []string{"main"}},
		"environment": "Production",
	})
	return WorkflowDefinition{
		Path: ".github/workflows/deploy.yml", Name: "Deploy", Enabled: true, State: "active",
		Push: &PushTrigger{Branches: []string{"main"}}, Environment: "Production",
		TriggerConfig: triggerConfig,
	}
}
