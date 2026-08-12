package platform

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/authz"
)

func TestAuthorizationIntegrationDirectPushRequiresLatestExactStatuses(t *testing.T) {
	fixture := authorizationIntegrationFixture(t)
	ctx := context.Background()
	resourceA := "urc-" + fixture.loreA
	sourceRevision := strings.Repeat("a", 64)
	otherRevision := strings.Repeat("b", 64)

	authorizationMustExec(t, fixture.pool, `
		INSERT INTO branch_rules (
			id, repository_id, pattern, required_approvals,
			required_status_checks, block_direct_push
		) VALUES
			($1, $2, '*', 0, ARRAY['CI/Test'], false),
			($3, $2, 'main', 0, ARRAY['ci/test', 'security'], false)
	`, uuid.New(), fixture.repositoryA, uuid.New())
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO repository_branch_states (
			repository_id, branch_id, branch_name, latest_revision
		) VALUES ($1, 'status-main-id', 'main', 'current-revision')
	`, fixture.repositoryA)

	check := authz.PolicyCheck{
		UserID: fixture.alice.ID, ResourceID: resourceA, Operation: authz.OperationBranchPush,
		BranchID: "status-main-id", ProposedRevision: sourceRevision,
	}
	assertPolicyDenied(t, ctx, fixture.store, check, "missing statuses")
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO revision_statuses (
			id, repository_id, revision, context, state, creator_id, created_at
		) VALUES
			($1, $2, $3, 'CI/Test', 'success', $4, now()),
			($5, $6, $7, 'security', 'success', $8, now()),
			($9, $2, $7, 'security', 'success', $4, now()),
			($10, $2, $3, 'security', 'pending', $4, now())
	`, uuid.New(), fixture.repositoryA, sourceRevision, fixture.alice.ID,
		uuid.New(), fixture.repositoryB, otherRevision, fixture.bob.ID,
		uuid.New(), uuid.New())
	assertPolicyDenied(t, ctx, fixture.store, check, "pending exact-revision status")
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO revision_statuses (
			id, repository_id, revision, context, state, creator_id, created_at
		) VALUES ($1, $2, $3, 'SECURITY', 'failure', $4, now() + interval '1 second')
	`, uuid.New(), fixture.repositoryA, sourceRevision, fixture.alice.ID)
	assertPolicyDenied(t, ctx, fixture.store, check, "latest failed status")
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO revision_statuses (
			id, repository_id, revision, context, state, creator_id, created_at
		) VALUES ($1, $2, $3, 'security', 'success', $4, now() + interval '2 seconds')
	`, uuid.New(), fixture.repositoryA, sourceRevision, fixture.alice.ID)
	decision, err := fixture.store.CheckPolicy(ctx, check)
	if err != nil || !decision.Allowed {
		t.Fatalf("successful exact-revision statuses decision = %+v, err %v", decision, err)
	}
}

func TestAuthorizationIntegrationProtectedPushRechecksSourceStatuses(t *testing.T) {
	fixture := authorizationIntegrationFixture(t)
	ctx := context.Background()
	resourceA := "urc-" + fixture.loreA
	sourceRevision := strings.Repeat("a", 64)
	targetRevision := strings.Repeat("b", 64)
	proposedRevision := strings.Repeat("c", 64)

	authorizationMustExec(t, fixture.pool, `
		INSERT INTO branch_rules (
			id, repository_id, pattern, required_approvals,
			required_status_checks, block_direct_push
		) VALUES ($1, $2, 'main', 0, ARRAY['CI/Test'], true)
	`, uuid.New(), fixture.repositoryA)
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO repository_branch_states (
			repository_id, branch_id, branch_name, latest_revision
		) VALUES ($1, 'protected-status-main', 'main', $2)
	`, fixture.repositoryA, targetRevision)
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO revision_statuses (
			id, repository_id, revision, context, state, creator_id
		) VALUES ($1, $2, $3, 'CI/Test', 'success', $4)
	`, uuid.New(), fixture.repositoryA, sourceRevision, fixture.alice.ID)
	prepareTestMergeAuthorization(t, fixture, ctx, fixture.alice.ID, MergeAuthorizationInput{
		RepositoryID:   fixture.loreA,
		BranchID:       "protected-status-main",
		BranchName:     "main",
		ExpectedBase:   targetRevision,
		ExpectedHead:   proposedRevision,
		SourceRevision: sourceRevision,
		Lifetime:       time.Minute,
	})
	check := authz.PolicyCheck{
		UserID: fixture.alice.ID, ResourceID: resourceA, Operation: authz.OperationBranchPush,
		BranchID: "protected-status-main", ProposedRevision: proposedRevision,
	}
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO revision_statuses (
			id, repository_id, revision, context, state, creator_id,
			created_at
		) VALUES ($1, $2, $3, 'ci/test', 'failure', $4, now() + interval '1 second')
	`, uuid.New(), fixture.repositoryA, sourceRevision, fixture.alice.ID)
	assertPolicyDenied(t, ctx, fixture.store, check, "status changed after merge authorization")
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO revision_statuses (
			id, repository_id, revision, context, state, creator_id,
			created_at
		) VALUES ($1, $2, $3, 'CI/Test', 'success', $4, now() + interval '2 seconds')
	`, uuid.New(), fixture.repositoryA, sourceRevision, fixture.alice.ID)
	decision, err := fixture.store.CheckPolicy(ctx, check)
	if err != nil || !decision.Allowed {
		t.Fatalf("restored source status decision = %+v, err %v", decision, err)
	}
	decision, err = fixture.store.CheckPolicy(ctx, check)
	if err != nil || decision.Allowed {
		t.Fatalf("consumed protected status authorization = %+v, err %v", decision, err)
	}
}

func assertPolicyDenied(
	t *testing.T,
	ctx context.Context,
	store *Store,
	check authz.PolicyCheck,
	reason string,
) {
	t.Helper()
	decision, err := store.CheckPolicy(ctx, check)
	if err != nil || decision.Allowed {
		t.Fatalf("%s decision = %+v, err %v", reason, decision, err)
	}
}
