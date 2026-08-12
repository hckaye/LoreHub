package collab

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

func TestIntegrationLorePushRequiresLatestExactRevisionStatuses(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "private", "write")
	other := setupFixture(t, pool, "private", "write")
	sourceRevision := strings.Repeat("a", 64)
	otherRevision := strings.Repeat("b", 64)
	targetRevision := strings.Repeat("c", 64)
	proposedRevision := strings.Repeat("d", 64)

	mustExec(t, ctx, pool, `
		INSERT INTO branch_rules (
			id, repository_id, pattern, required_approvals, require_ci_success,
			required_status_checks, block_direct_push
		) VALUES
			($1, $2, '*', 0, false, ARRAY['CI/Test'], true),
			($3, $2, 'main', 0, false, ARRAY['lint', 'security'], true)
	`, uuidNew(), fixture.repoID, uuidNew())
	mustExec(t, ctx, pool, `
		INSERT INTO revision_statuses (
			id, repository_id, revision, context, state, creator_id, created_at
		) VALUES
			($1, $2, $3, 'CI/Test', 'failure', $4, now() - interval '2 minutes'),
			($5, $2, $3, 'ci/test', 'success', $4, now() - interval '1 minute'),
			($6, $2, $3, 'lint', 'pending', $4, now()),
			($7, $2, $8, 'security', 'success', $4, now()),
			($9, $10, $3, 'security', 'success', $11, now())
	`, uuidNew(), fixture.repoID, sourceRevision, fixture.alice.ID,
		uuidNew(), uuidNew(), uuidNew(), otherRevision, uuidNew(), other.repoID, other.alice.ID)

	checks, err := store.ListRevisionStatusChecks(ctx, fixture.repoID, sourceRevision)
	if err != nil {
		t.Fatalf("list latest exact revision statuses: %v", err)
	}
	if len(checks) != 2 || checks[0].Context != "ci/test" || checks[0].State != "success" ||
		checks[1].Context != "lint" || checks[1].State != "pending" {
		t.Fatalf("latest exact revision statuses = %#v", checks)
	}

	authorization := prepareStatusCheckPushAuthorization(
		t,
		ctx,
		store,
		fixture,
		sourceRevision,
		targetRevision,
		proposedRevision,
	)
	assertPushAuthorizationDenied(t, ctx, store, authorization, "pending and missing checks")

	mustExec(t, ctx, pool, `
		INSERT INTO revision_statuses (
			id, repository_id, revision, context, state, creator_id, created_at
		) VALUES ($1, $2, $3, 'LINT', 'failure', $4, now() + interval '1 second')
	`, uuidNew(), fixture.repoID, sourceRevision, fixture.alice.ID)
	assertPushAuthorizationDenied(t, ctx, store, authorization, "latest failed check")

	mustExec(t, ctx, pool, `
		INSERT INTO revision_statuses (
			id, repository_id, revision, context, state, creator_id, created_at
		) VALUES ($1, $2, $3, 'lint', 'success', $4, now() + interval '2 seconds')
	`, uuidNew(), fixture.repoID, sourceRevision, fixture.alice.ID)
	assertPushAuthorizationDenied(t, ctx, store, authorization, "missing check")

	mustExec(t, ctx, pool, `
		INSERT INTO revision_statuses (
			id, repository_id, revision, context, state, creator_id, created_at
		) VALUES ($1, $2, $3, 'SECURITY', 'success', $4, now() + interval '3 seconds')
	`, uuidNew(), fixture.repoID, sourceRevision, fixture.alice.ID)
	if err := store.AuthorizeLoreMergePush(ctx, authorization); err != nil {
		t.Fatalf("successful required status checks were denied: %v", err)
	}
	mustExec(t, ctx, pool, `
		INSERT INTO revision_statuses (
			id, repository_id, revision, context, state, creator_id, created_at
		) VALUES ($1, $2, $3, 'security', 'error', $4, now() + interval '4 seconds')
	`, uuidNew(), fixture.repoID, sourceRevision, fixture.alice.ID)
	assertPushAuthorizationDenied(t, ctx, store, authorization, "status changed after prior authorization")
}

func prepareStatusCheckPushAuthorization(
	t *testing.T,
	ctx context.Context,
	store *store,
	fixture integrationFixture,
	sourceRevision string,
	targetRevision string,
	proposedRevision string,
) loreclient.PushAuthorization {
	t.Helper()
	number := seedMergeRequest(t, ctx, store.pool, fixture, fixture.alice.ID, sourceRevision)
	branchID := "branch-required-statuses"
	mustExec(t, ctx, store.pool, `
		INSERT INTO repository_branch_states (
			repository_id, branch_id, branch_name, latest_revision
		) VALUES ($1, $2, 'main', $3)
	`, fixture.repoID, branchID, targetRevision)
	operation, err := store.AcquireMergeOperation(
		ctx,
		fixture.alice.ID,
		fixture.repoID,
		number,
		sourceRevision,
		targetRevision,
		"status-check-owner",
		time.Minute,
	)
	if err != nil {
		t.Fatalf("acquire status-check merge operation: %v", err)
	}
	operation.State = "pushing"
	operation.StagedRevision = proposedRevision
	operation.ParentRevisions = []string{sourceRevision, targetRevision}
	operation, err = store.UpdateMergeOperation(ctx, operation)
	if err != nil {
		t.Fatalf("mark status-check merge operation pushing: %v", err)
	}
	return loreclient.PushAuthorization{
		ActorUserID:            fixture.alice.ID,
		RepositoryID:           fixture.repoID,
		RepositoryPartition:    loreFixtureID(fixture.orgID),
		OperationID:            operation.ID,
		TargetBranchID:         branchID,
		TargetBranchName:       "main",
		ExpectedTargetRevision: targetRevision,
		ProposedRevision:       proposedRevision,
		SourceRevision:         sourceRevision,
		ParentRevisions:        []string{sourceRevision, targetRevision},
	}
}

func assertPushAuthorizationDenied(
	t *testing.T,
	ctx context.Context,
	store *store,
	authorization loreclient.PushAuthorization,
	reason string,
) {
	t.Helper()
	if err := store.AuthorizeLoreMergePush(ctx, authorization); !errors.Is(
		err,
		loreclient.ErrPushAuthorizationDenied,
	) {
		t.Fatalf("%s authorization error = %v, want denial", reason, err)
	}
}
