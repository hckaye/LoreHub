package collab

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestIntegrationMergeRequestDraftAuthorizationAuditAndState(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "private", "read")
	number := seedMergeRequest(t, ctx, pool, fixture, fixture.alice.ID, "draft-source")

	if _, _, err := store.SetMergeRequestDraft(
		ctx, fixture.bob, fixture.repoID, number, true,
	); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("reader draft update error = %v, want ErrForbidden", err)
	}
	if _, _, err := store.SetMergeRequestDraft(
		ctx, fixture.carol, fixture.repoID, number, true,
	); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("unprivileged member draft update error = %v, want ErrForbidden", err)
	}
	authorNumber := seedMergeRequest(t, ctx, pool, fixture, fixture.bob.ID, "draft-reader-author")
	if draft, changed, err := store.SetMergeRequestDraft(
		ctx, fixture.bob, fixture.repoID, authorNumber, true,
	); err != nil || !changed || !draft.IsDraft {
		t.Fatalf("reader author draft update = %+v changed=%t err=%v", draft, changed, err)
	}
	mustExec(t, ctx, pool, `
		UPDATE repository_memberships SET role = 'triage'
		WHERE repository_id = $1 AND user_id = $2
	`, fixture.repoID, fixture.bob.ID)
	if draft, changed, err := store.SetMergeRequestDraft(
		ctx, fixture.bob, fixture.repoID, number, true,
	); err != nil || !changed || !draft.IsDraft {
		t.Fatalf("triage draft update = %+v changed=%t err=%v", draft, changed, err)
	}
	if _, _, err := store.SetMergeRequestDraft(ctx, fixture.bob, fixture.repoID, number, false); err != nil {
		t.Fatalf("triage mark ready: %v", err)
	}

	convertAuditBefore := countAuditAction(t, ctx, pool, "merge_request.convert_to_draft")
	updatedTopicBefore := countTopic(t, ctx, pool, "merge_request.updated")
	draft, changed, err := store.SetMergeRequestDraft(ctx, fixture.alice, fixture.repoID, number, true)
	if err != nil || !changed || !draft.IsDraft {
		t.Fatalf("convert to draft = %+v changed=%t err=%v", draft, changed, err)
	}
	if got := countAuditAction(t, ctx, pool, "merge_request.convert_to_draft"); got != convertAuditBefore+1 {
		t.Fatalf("convert audit count = %d, want %d", got, convertAuditBefore+1)
	}
	if got := countTopic(t, ctx, pool, "merge_request.updated"); got != updatedTopicBefore+1 {
		t.Fatalf("draft update topic count = %d, want %d", got, updatedTopicBefore+1)
	}

	draft, changed, err = store.SetMergeRequestDraft(ctx, fixture.alice, fixture.repoID, number, true)
	if err != nil || changed || !draft.IsDraft {
		t.Fatalf("repeat draft update = %+v changed=%t err=%v", draft, changed, err)
	}
	if got := countAuditAction(t, ctx, pool, "merge_request.convert_to_draft"); got != convertAuditBefore+1 {
		t.Fatalf("repeat convert audit count = %d, want %d", got, convertAuditBefore+1)
	}
	if got := countTopic(t, ctx, pool, "merge_request.updated"); got != updatedTopicBefore+1 {
		t.Fatalf("repeat draft topic count = %d, want %d", got, updatedTopicBefore+1)
	}

	readyAuditBefore := countAuditAction(t, ctx, pool, "merge_request.mark_ready")
	ready, changed, err := store.SetMergeRequestDraft(ctx, fixture.alice, fixture.repoID, number, false)
	if err != nil || !changed || ready.IsDraft {
		t.Fatalf("mark ready = %+v changed=%t err=%v", ready, changed, err)
	}
	if got := countAuditAction(t, ctx, pool, "merge_request.mark_ready"); got != readyAuditBefore+1 {
		t.Fatalf("ready audit count = %d, want %d", got, readyAuditBefore+1)
	}

	mustExec(t, ctx, pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, fixture.alice.ID)
	if _, _, err := store.SetMergeRequestDraft(
		ctx, fixture.alice, fixture.repoID, number, true,
	); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("suspended author draft update error = %v, want ErrForbidden", err)
	}
	mustExec(t, ctx, pool, `UPDATE organizations SET active = false WHERE id = $1`, fixture.orgID)
	if _, _, err := store.SetMergeRequestDraft(
		ctx, fixture.bob, fixture.repoID, number, true,
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("inactive organization draft update error = %v, want ErrNotFound", err)
	}
}

func TestIntegrationMergeRequestDraftRejectsMergeRaces(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "public", "write")
	number := seedMergeRequest(t, ctx, pool, fixture, fixture.alice.ID, "draft-race-source")

	operation, err := store.AcquireMergeOperation(ctx, fixture.alice.ID, fixture.repoID, number,
		"draft-race-source", "main-rev", "draft-race-owner", time.Minute)
	if err != nil {
		t.Fatalf("acquire merge operation: %v", err)
	}
	operation.State = "pushing"
	operation.StagedRevision = "draft-race-staged"
	if _, err := store.UpdateMergeOperation(ctx, operation); err != nil {
		t.Fatalf("mark merge operation pushing: %v", err)
	}
	if _, _, err := store.SetMergeRequestDraft(
		ctx, fixture.alice, fixture.repoID, number, true,
	); !errors.Is(err, ErrMergeBusy) {
		t.Fatalf("draft during merge error = %v, want ErrMergeBusy", err)
	}

	mustExec(t, ctx, pool, `UPDATE merge_operations SET state = 'aborted' WHERE id = $1`, operation.ID)
	if _, _, err := store.SetMergeRequestDraft(
		ctx, fixture.alice, fixture.repoID, number, true,
	); err != nil {
		t.Fatalf("convert to draft after failed merge: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE merge_requests SET state = 'merged' WHERE repository_id = $1 AND number = $2
	`, fixture.repoID, number); err == nil {
		t.Fatal("database accepted a merged draft pull request")
	}

	mustExec(t, ctx, pool, `
		UPDATE merge_requests SET is_draft = false, draft_changed_at = NULL, draft_changed_by = NULL,
		state = 'closed' WHERE repository_id = $1 AND number = $2
	`, fixture.repoID, number)
	if _, _, err := store.SetMergeRequestDraft(
		ctx, fixture.alice, fixture.repoID, number, true,
	); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("closed draft update error = %v, want ErrConflict", err)
	}
}

func TestIntegrationMergeFinalizationRejectsDraftChangedAfterPush(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "public", "write")
	number := seedMergeRequest(t, ctx, pool, fixture, fixture.alice.ID, "draft-finalize-source")

	operation, err := store.AcquireMergeOperation(ctx, fixture.alice.ID, fixture.repoID, number,
		"draft-finalize-source", "main-rev", "draft-finalize-owner", time.Minute)
	if err != nil {
		t.Fatalf("acquire merge operation: %v", err)
	}
	operation.State = "ready_to_push"
	operation.StagedRevision = "draft-finalize-staged"
	operation = mustUpdateMergeOperation(t, ctx, store, operation)
	operation.State = "pushed"
	operation.PushedRevision = "draft-finalize-pushed"
	operation = mustUpdateMergeOperation(t, ctx, store, operation)

	mustExec(t, ctx, pool, `
		UPDATE merge_requests
		SET is_draft = true, draft_changed_at = now(), draft_changed_by = $3
		WHERE repository_id = $1 AND number = $2
	`, fixture.repoID, number, fixture.alice.ID)
	if _, err := store.FinalizeMerged(
		ctx, fixture.alice, fixture.repoID, number, operation.ID, "draft-finalize-pushed",
	); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("draft finalization error = %v, want ErrConflict", err)
	}
	mergeRequest, err := store.GetMergeRequest(ctx, fixture.repoID, number)
	if err != nil {
		t.Fatalf("load draft after rejected finalization: %v", err)
	}
	if mergeRequest.State != "open" || !mergeRequest.IsDraft {
		t.Fatalf("pull request after rejected finalization = %+v", mergeRequest)
	}
	operation, err = store.GetMergeOperation(ctx, fixture.repoID, number)
	if err != nil {
		t.Fatalf("load operation after rejected finalization: %v", err)
	}
	if operation.State != "pushed" {
		t.Fatalf("operation state after rejected finalization = %q, want pushed", operation.State)
	}
}
