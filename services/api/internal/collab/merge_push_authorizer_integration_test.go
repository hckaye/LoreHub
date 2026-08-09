package collab

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestIntegrationLorePushAuthorizationIsAtomicAndIdempotent(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "private", "write")
	number := seedMergeRequest(t, ctx, pool, fixture, fixture.alice.ID, "source-auth")
	if _, _, err := store.CreateReview(ctx, fixture.bob, fixture.repoID, number,
		ReviewInput{Decision: "approved"}); err != nil {
		t.Fatalf("create approval: %v", err)
	}
	mustExec(t, ctx, pool, `
		INSERT INTO branch_rules (
			id, repository_id, pattern, required_approvals, require_ci_success, block_direct_push
		) VALUES ($1, $2, 'main', 1, true, true)
	`, uuidNew(), fixture.repoID)
	mustExec(t, ctx, pool, `
		INSERT INTO ci_runs (
			id, repository_id, run_number, event_name, branch, revision, status, conclusion, event_payload
		) VALUES ($1, $2, 1, 'push', 'feature', 'source-auth', 'completed', 'success', '{}')
	`, uuidNew(), fixture.repoID)
	mustExec(t, ctx, pool, `
		INSERT INTO repository_branch_states (
			repository_id, branch_id, branch_name, latest_revision
		) VALUES ($1, 'branch-main', 'main', 'main-rev')
	`, fixture.repoID)
	operation, err := store.AcquireMergeOperation(ctx, fixture.alice.ID, fixture.repoID, number,
		"source-auth", "main-rev", "push-owner", time.Minute)
	if err != nil {
		t.Fatalf("acquire operation: %v", err)
	}
	operation.State = "pushing"
	operation.StagedRevision = "merged-auth"
	operation, err = store.UpdateMergeOperation(ctx, operation)
	if err != nil {
		t.Fatalf("mark operation pushing: %v", err)
	}
	authorization := loreclient.PushAuthorization{
		ActorUserID:            fixture.alice.ID,
		RepositoryID:           fixture.repoID,
		RepositoryPartition:    loreFixtureID(fixture.orgID),
		OperationID:            operation.ID,
		TargetBranchID:         "branch-main",
		TargetBranchName:       "main",
		ExpectedTargetRevision: "main-rev",
		ProposedRevision:       "merged-auth",
		SourceRevision:         "source-auth",
		ParentRevisions:        []string{"source-auth", "main-rev"},
	}
	for name, wrong := range map[string]loreclient.PushAuthorization{
		"actor": func() loreclient.PushAuthorization {
			value := authorization
			value.ActorUserID = fixture.bob.ID
			return value
		}(),
		"base": func() loreclient.PushAuthorization {
			value := authorization
			value.ExpectedTargetRevision = "wrong-target"
			return value
		}(),
		"branch": func() loreclient.PushAuthorization {
			value := authorization
			value.TargetBranchID = "wrong-branch"
			return value
		}(),
		"proposed": func() loreclient.PushAuthorization {
			value := authorization
			value.ProposedRevision = "wrong-proposed"
			return value
		}(),
	} {
		if err := store.AuthorizeLoreMergePush(ctx, wrong); !errors.Is(err, loreclient.ErrPushAuthorizationDenied) {
			t.Fatalf("wrong %s authorization error = %v, want denial", name, err)
		}
	}
	auditBefore := countAuditAction(t, ctx, pool, "merge_operation.push_authorized")
	outboxBefore := countTopic(t, ctx, pool, "merge_operation.push_authorized")
	if err := store.AuthorizeLoreMergePush(ctx, authorization); err != nil {
		t.Fatalf("authorize Lore push: %v", err)
	}
	if err := store.AuthorizeLoreMergePush(ctx, authorization); err != nil {
		t.Fatalf("repeat Lore push authorization: %v", err)
	}
	if got := countAuditAction(t, ctx, pool, "merge_operation.push_authorized"); got != auditBefore+1 {
		t.Fatalf("push authorization audit count = %d, want %d", got, auditBefore+1)
	}
	if got := countTopic(t, ctx, pool, "merge_operation.push_authorized"); got != outboxBefore+1 {
		t.Fatalf("push authorization outbox count = %d, want %d", got, outboxBefore+1)
	}

	wrong := authorization
	wrong.ProposedRevision = "another-proposed-revision"
	if err := store.AuthorizeLoreMergePush(ctx, wrong); !errors.Is(err, loreclient.ErrPushAuthorizationDenied) {
		t.Fatalf("wrong proposed revision error = %v, want denial", err)
	}

	operation, err = store.GetMergeOperation(ctx, fixture.repoID, number)
	if err != nil {
		t.Fatalf("get authorized operation: %v", err)
	}
	if len(operation.ParentRevisions) != 2 || operation.ParentRevisions[0] != "source-auth" ||
		operation.ParentRevisions[1] != "main-rev" {
		t.Fatalf("authorized parent revisions = %#v, want exact SDK parents", operation.ParentRevisions)
	}
	mustExec(t, ctx, pool, `UPDATE merge_operations SET lease_expires_at = now() - interval '1 second' WHERE id = $1`,
		operation.ID)
	if err := store.AuthorizeLoreMergePush(ctx, authorization); !errors.Is(err, loreclient.ErrPushAuthorizationDenied) {
		t.Fatalf("expired lease authorization error = %v, want denial", err)
	}
}

func TestIntegrationLorePushAuthorizationSerializesConcurrentConsumers(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "private", "write")
	number := seedMergeRequest(t, ctx, pool, fixture, fixture.alice.ID, "source-concurrent-auth")
	if _, _, err := store.CreateReview(ctx, fixture.bob, fixture.repoID, number,
		ReviewInput{Decision: "approved"}); err != nil {
		t.Fatalf("create approval: %v", err)
	}
	mustExec(t, ctx, pool, `
		INSERT INTO repository_branch_states (repository_id, branch_id, branch_name, latest_revision)
		VALUES ($1, 'branch-concurrent', 'main', 'main-concurrent')
	`, fixture.repoID)
	operation, err := store.AcquireMergeOperation(ctx, fixture.alice.ID, fixture.repoID, number,
		"source-concurrent-auth", "main-concurrent", "concurrent-owner", time.Minute)
	if err != nil {
		t.Fatalf("acquire operation: %v", err)
	}
	operation.State = "pushing"
	operation.StagedRevision = "merged-concurrent"
	operation.ParentRevisions = []string{"source-concurrent-auth", "main-concurrent"}
	operation, err = store.UpdateMergeOperation(ctx, operation)
	if err != nil {
		t.Fatalf("mark operation pushing: %v", err)
	}
	authorization := loreclient.PushAuthorization{
		ActorUserID:            fixture.alice.ID,
		RepositoryID:           fixture.repoID,
		RepositoryPartition:    loreFixtureID(fixture.orgID),
		OperationID:            operation.ID,
		TargetBranchID:         "branch-concurrent",
		TargetBranchName:       "main",
		ExpectedTargetRevision: "main-concurrent",
		ProposedRevision:       "merged-concurrent",
		SourceRevision:         "source-concurrent-auth",
		ParentRevisions:        []string{"source-concurrent-auth", "main-concurrent"},
	}
	auditBefore := countAuditAction(t, ctx, pool, "merge_operation.push_authorized")
	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			results <- store.AuthorizeLoreMergePush(ctx, authorization)
		}()
	}
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent push authorization: %v", err)
		}
	}
	if got := countAuditAction(t, ctx, pool, "merge_operation.push_authorized"); got != auditBefore+1 {
		t.Fatalf("concurrent push authorization audit count = %d, want %d", got, auditBefore+1)
	}
}

func TestIntegrationLorePushPermissionRequiresActiveRepositoryGrant(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "private", "")
	teamID := uuidNew()
	mustExec(t, ctx, pool, `
		INSERT INTO teams (id, organization_id, slug, display_name, created_by, active)
		VALUES ($1, $2, 'merge-team', 'Merge Team', $3, true)
	`, teamID, fixture.orgID, fixture.alice.ID)
	mustExec(t, ctx, pool, `
		INSERT INTO team_memberships (team_id, user_id, active) VALUES ($1, $2, true)
	`, teamID, fixture.bob.ID)
	mustExec(t, ctx, pool, `
		INSERT INTO team_repository_roles (team_id, repository_id, role, created_by, active)
		VALUES ($1, $2, 'maintain', $3, true)
	`, teamID, fixture.repoID, fixture.alice.ID)

	newAuthorization := func(actor platform.User, revision string) loreclient.PushAuthorization {
		number := seedMergeRequest(t, ctx, pool, fixture, actor.ID, revision)
		operation, err := store.AcquireMergeOperation(ctx, actor.ID, fixture.repoID, number,
			revision, "main-rev", "permission-owner-"+revision, time.Minute)
		if err != nil {
			t.Fatalf("acquire permission operation: %v", err)
		}
		operation.State = "pushing"
		operation.StagedRevision = "merged-" + revision
		operation.ParentRevisions = []string{revision, "main-rev"}
		if _, err := store.UpdateMergeOperation(ctx, operation); err != nil {
			t.Fatalf("mark permission operation pushing: %v", err)
		}
		branchID := "branch-" + revision
		mustExec(t, ctx, pool, `
			INSERT INTO repository_branch_states (repository_id, branch_id, branch_name, latest_revision)
			VALUES ($1, $2, 'main', 'main-rev')
		`, fixture.repoID, branchID)
		return loreclient.PushAuthorization{
			ActorUserID:            actor.ID,
			RepositoryID:           fixture.repoID,
			RepositoryPartition:    loreFixtureID(fixture.orgID),
			OperationID:            operation.ID,
			TargetBranchID:         branchID,
			TargetBranchName:       "main",
			ExpectedTargetRevision: "main-rev",
			ProposedRevision:       "merged-" + revision,
			SourceRevision:         revision,
			ParentRevisions:        []string{revision, "main-rev"},
		}
	}

	teamAuthorization := newAuthorization(fixture.bob, "team-maintain")
	if err := store.AuthorizeLoreMergePush(ctx, teamAuthorization); err != nil {
		t.Fatalf("active team maintain grant was denied: %v", err)
	}
	staleAuthorization := newAuthorization(fixture.bob, "stale-observation")
	mustExec(t, ctx, pool, `
		UPDATE repository_branch_states SET observed_at = now() - interval '2 minutes 1 second'
		WHERE repository_id = $1 AND branch_id = $2
	`, fixture.repoID, staleAuthorization.TargetBranchID)
	if err := store.AuthorizeLoreMergePush(ctx, staleAuthorization); !errors.Is(
		err, loreclient.ErrPushAuthorizationDenied) {
		t.Fatalf("stale branch observation error = %v, want denial", err)
	}
	mustExec(t, ctx, pool, `
		UPDATE organization_memberships SET active = false
		WHERE organization_id = $1 AND user_id = $2
	`, fixture.orgID, fixture.bob.ID)
	inactiveAuthorization := newAuthorization(fixture.bob, "inactive-membership")
	if err := store.AuthorizeLoreMergePush(ctx, inactiveAuthorization); !errors.Is(
		err, loreclient.ErrPushAuthorizationDenied) {
		t.Fatalf("inactive organization membership error = %v, want denial", err)
	}
	mustExec(t, ctx, pool, `
		UPDATE organization_memberships SET active = true
		WHERE organization_id = $1 AND user_id = $2
	`, fixture.orgID, fixture.bob.ID)
	mustExec(t, ctx, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role, active)
		VALUES ($1, $2, 'maintain', true)
	`, fixture.repoID, fixture.carol.ID)
	directAuthorization := newAuthorization(fixture.carol, "direct-maintain")
	if err := store.AuthorizeLoreMergePush(ctx, directAuthorization); err != nil {
		t.Fatalf("active direct maintain grant was denied: %v", err)
	}
	mustExec(t, ctx, pool, `
		UPDATE organization_memberships SET role = 'maintainer' WHERE organization_id = $1 AND user_id = $2
	`, fixture.orgID, fixture.carol.ID)
	mustExec(t, ctx, pool, `
		UPDATE repository_memberships SET active = false WHERE repository_id = $1 AND user_id = $2
	`, fixture.repoID, fixture.carol.ID)
	maintainerAuthorization := newAuthorization(fixture.carol, "org-maintainer-only")
	if err := store.AuthorizeLoreMergePush(ctx, maintainerAuthorization); !errors.Is(
		err, loreclient.ErrPushAuthorizationDenied) {
		t.Fatalf("organization maintainer-only grant error = %v, want denial", err)
	}
	mustExec(t, ctx, pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, fixture.bob.ID)
	suspendedAuthorization := newAuthorization(fixture.bob, "suspended-team")
	if err := store.AuthorizeLoreMergePush(ctx, suspendedAuthorization); !errors.Is(
		err, loreclient.ErrPushAuthorizationDenied) {
		t.Fatalf("suspended team member error = %v, want denial", err)
	}
}

func TestIntegrationLorePushAuthorizationReconcilesResolutionReplay(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "private", "write")
	number := seedMergeRequest(t, ctx, pool, fixture, fixture.alice.ID, "source-replay-auth")
	mustExec(t, ctx, pool, `
		INSERT INTO repository_branch_states (repository_id, branch_id, branch_name, latest_revision)
		VALUES ($1, 'branch-replay', 'main', 'main-rev')
	`, fixture.repoID)
	operation, err := store.AcquireMergeOperation(ctx, fixture.alice.ID, fixture.repoID, number,
		"source-replay-auth", "main-rev", "replay-owner", time.Minute)
	if err != nil {
		t.Fatalf("acquire replay operation: %v", err)
	}
	operation.State = "pushing"
	operation.StagedRevision = "staged-before-replay"
	operation.ParentRevisions = []string{"source-replay-auth", "main-rev"}
	operation, err = store.UpdateMergeOperation(ctx, operation)
	if err != nil {
		t.Fatalf("mark replay operation pushing: %v", err)
	}
	mustExec(t, ctx, pool, `
		INSERT INTO merge_operation_resolutions (operation_id, path, strategy, actor_id)
		VALUES ($1, 'conflict.txt', 'theirs', $2)
	`, operation.ID, fixture.alice.ID)
	authorization := loreclient.PushAuthorization{
		ActorUserID:            fixture.alice.ID,
		RepositoryID:           fixture.repoID,
		RepositoryPartition:    loreFixtureID(fixture.orgID),
		OperationID:            operation.ID,
		TargetBranchID:         "branch-replay",
		TargetBranchName:       "main",
		ExpectedTargetRevision: "main-rev",
		ProposedRevision:       "replayed-merge",
		SourceRevision:         "source-replay-auth",
		ParentRevisions:        []string{"source-replay-auth", "main-rev"},
	}
	auditBefore := countAuditAction(t, ctx, pool, "merge_operation.push_authorized")
	outboxBefore := countTopic(t, ctx, pool, "merge_operation.push_authorized")
	if err := store.AuthorizeLoreMergePush(ctx, authorization); err != nil {
		t.Fatalf("authorize replayed Lore push: %v", err)
	}
	reconciled, err := store.GetMergeOperation(ctx, fixture.repoID, number)
	if err != nil {
		t.Fatalf("get reconciled operation: %v", err)
	}
	if reconciled.StagedRevision != authorization.ProposedRevision {
		t.Fatalf("reconciled staged revision = %q, want %q", reconciled.StagedRevision,
			authorization.ProposedRevision)
	}
	if err := store.AuthorizeLoreMergePush(ctx, authorization); err != nil {
		t.Fatalf("repeat replayed Lore push authorization: %v", err)
	}
	if got := countAuditAction(t, ctx, pool, "merge_operation.push_authorized"); got != auditBefore+1 {
		t.Fatalf("replayed authorization audit count = %d, want %d", got, auditBefore+1)
	}
	if got := countTopic(t, ctx, pool, "merge_operation.push_authorized"); got != outboxBefore+1 {
		t.Fatalf("replayed authorization outbox count = %d, want %d", got, outboxBefore+1)
	}

	noChoiceNumber := seedMergeRequest(t, ctx, pool, fixture, fixture.alice.ID, "source-no-replay")
	noChoice, err := store.AcquireMergeOperation(ctx, fixture.alice.ID, fixture.repoID, noChoiceNumber,
		"source-no-replay", "main-rev", "no-replay-owner", time.Minute)
	if err != nil {
		t.Fatalf("acquire no-replay operation: %v", err)
	}
	noChoice.State = "pushing"
	noChoice.StagedRevision = "staged-without-replay"
	noChoice.ParentRevisions = []string{"source-no-replay", "main-rev"}
	if _, err := store.UpdateMergeOperation(ctx, noChoice); err != nil {
		t.Fatalf("mark no-replay operation pushing: %v", err)
	}
	noReplayAuthorization := authorization
	noReplayAuthorization.OperationID = noChoice.ID
	noReplayAuthorization.SourceRevision = "source-no-replay"
	noReplayAuthorization.ProposedRevision = "unreconciled-merge"
	if err := store.AuthorizeLoreMergePush(ctx, noReplayAuthorization); !errors.Is(
		err, loreclient.ErrPushAuthorizationDenied) {
		t.Fatalf("unreconciled Lore push authorization error = %v, want denial", err)
	}
	unchanged, err := store.GetMergeOperation(ctx, fixture.repoID, noChoiceNumber)
	if err != nil {
		t.Fatalf("get unreconciled operation: %v", err)
	}
	if unchanged.StagedRevision != "staged-without-replay" {
		t.Fatalf("unreconciled staged revision = %q, want unchanged value", unchanged.StagedRevision)
	}
}
