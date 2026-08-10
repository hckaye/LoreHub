package runner

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/database"
)

func TestPostgresJobTokenLifecycle(t *testing.T) {
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
	runID := uuid.NewString()
	jobID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO ci_runs (
			id, repository_id, run_number, event_name, branch, revision,
			actor_id, status, event_payload, started_at
		) VALUES ($1, $2, 1, 'workflow_dispatch', 'main', 'revision-one',
			$3, 'in_progress', '{}', now())
	`, runID, fixture.repositoryID, fixture.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO ci_jobs (
			id, run_id, name, status, attempt, lease_owner, lease_expires_at, started_at
		) VALUES ($1, $2, 'build', 'in_progress', 1, 'runner-one', now() + interval '5 minutes', now())
	`, jobID, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO service_principal_repository_grants (
			principal_id, repository_id, permissions, active, created_by
		)
		SELECT id, $1, ARRAY['read']::varchar[], true, $2
		FROM service_principals
		WHERE name = 'lorehub-ci-runner' AND kind = 'ci_runner'
		ON CONFLICT (principal_id, repository_id) DO UPDATE
		SET permissions = EXCLUDED.permissions, active = true, updated_at = now()
	`, fixture.repositoryID, fixture.userID); err != nil {
		t.Fatal(err)
	}

	service, err := NewPostgresJobTokenService(
		pool,
		newJobTokenTestKeys(t, "actions-integration"),
		"https://lorehub.test/actions",
		"lorehub-actions",
	)
	if err != nil {
		t.Fatal(err)
	}
	request := JobTokenRequest{
		JobID:        jobID,
		RunID:        runID,
		Attempt:      1,
		RepositoryID: fixture.repositoryID,
		ActorID:      fixture.userID,
		ServicePrincipal: CredentialPrincipal{
			Kind: "service", Subject: "00000000-0000-4000-8000-000000000002",
		},
		RESTScope:       "actions:job:rest",
		GraphQLScope:    "actions:job:graphql",
		RequestedExpiry: time.Now().UTC().Add(10 * time.Minute),
	}
	issued, err := service.Issue(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := service.Verify(ctx, issued.Token, request.RESTScope, request.GraphQLScope)
	if err != nil || verified.Claims.JobID != jobID || verified.Claims.RepositoryID != fixture.repositoryID {
		t.Fatalf("valid database-backed job token was rejected: %#v, %v", verified, err)
	}

	mismatched := request
	mismatched.Attempt++
	if _, err := service.Issue(ctx, mismatched); !errors.Is(err, ErrActionsJobTokenUnauthorized) {
		t.Fatalf("mismatched job attempt returned %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE service_principal_repository_grants repository_grant
		SET active = false, updated_at = now()
		FROM service_principals principal
		WHERE repository_grant.principal_id = principal.id
		  AND repository_grant.repository_id = $1
		  AND principal.name = 'lorehub-ci-runner'
	`, fixture.repositoryID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(
		ctx, issued.Token, request.RESTScope, request.GraphQLScope,
	); !errors.Is(err, ErrActionsJobTokenUnauthorized) {
		t.Fatalf("revoked repository grant returned %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE service_principal_repository_grants repository_grant
		SET active = true, updated_at = now()
		FROM service_principals principal
		WHERE repository_grant.principal_id = principal.id
		  AND repository_grant.repository_id = $1
		  AND principal.name = 'lorehub-ci-runner'
	`, fixture.repositoryID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE ci_jobs SET lease_expires_at = now() - interval '1 second' WHERE id = $1
	`, jobID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(
		ctx, issued.Token, request.RESTScope, request.GraphQLScope,
	); !errors.Is(err, ErrActionsJobTokenUnauthorized) {
		t.Fatalf("expired job lease returned %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE ci_jobs SET lease_expires_at = now() + interval '5 minutes' WHERE id = $1
	`, jobID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE organizations SET active = false WHERE id = $1`,
		fixture.organizationID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Issue(ctx, request); !errors.Is(err, ErrActionsJobTokenUnauthorized) {
		t.Fatalf("inactive organization returned %v", err)
	}
}
