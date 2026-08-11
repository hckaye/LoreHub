package platform

import (
	"context"
	"testing"
	"time"
)

func TestCreateDraftMergeRequestPersistsDraftMetadata(t *testing.T) {
	fixture := authorizationIntegrationFixture(t)
	ctx := context.Background()
	created, err := fixture.store.CreateMergeRequest(ctx, fixture.alice, fixture.orgSlug, "a",
		CreateMergeRequestInput{
			Title: "Draft change", IsDraft: true,
			SourceBranch: "feature", TargetBranch: "main",
			SourceRevision: "draft-source", TargetRevision: "draft-target",
		})
	if err != nil {
		t.Fatalf("create draft pull request: %v", err)
	}
	if !created.IsDraft {
		t.Fatalf("created pull request = %+v, want draft", created)
	}
	var isDraft bool
	var changedAt *time.Time
	var changedBy *string
	if err := fixture.pool.QueryRow(ctx, `
		SELECT is_draft, draft_changed_at, draft_changed_by
		FROM merge_requests WHERE id = $1 AND repository_id = $2
	`, created.ID, fixture.repositoryA).Scan(&isDraft, &changedAt, &changedBy); err != nil {
		t.Fatalf("load draft metadata: %v", err)
	}
	if !isDraft || changedAt == nil || changedBy == nil || *changedBy != fixture.alice.ID {
		t.Fatalf("draft metadata: draft=%t changedAt=%v changedBy=%v", isDraft, changedAt, changedBy)
	}
	repository, err := fixture.store.RepositoryForRead(ctx, &fixture.alice, fixture.orgSlug, "a")
	if err != nil {
		t.Fatalf("load draft repository: %v", err)
	}
	listed, err := fixture.store.ListMergeRequestsForRead(ctx, &fixture.alice, repository, "open")
	if err != nil {
		t.Fatalf("list draft pull requests: %v", err)
	}
	found := false
	for _, mergeRequest := range listed {
		if mergeRequest.ID == created.ID {
			found = mergeRequest.IsDraft
		}
	}
	if !found {
		t.Fatalf("draft pull request %q was not returned as a draft", created.ID)
	}
}
