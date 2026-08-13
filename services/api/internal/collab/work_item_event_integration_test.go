package collab

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestIntegrationIssueMutationsRecordTimelineEvents(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "public", "triage")
	number := seedIssue(t, ctx, pool, fixture, fixture.alice.ID, "open")
	label, err := store.CreateLabel(
		ctx, fixture.alice, fixture.repoID, LabelInput{Name: "bug", Color: "d73a4a"},
	)
	if err != nil {
		t.Fatalf("create label: %v", err)
	}

	if _, err := store.UpdateIssue(ctx, fixture.alice, fixture.repoID, number, UpdateIssueInput{
		Title: ptrString("Renamed issue"), State: ptrString("closed"),
	}); err != nil {
		t.Fatalf("close issue: %v", err)
	}
	if _, err := store.UpdateIssue(ctx, fixture.alice, fixture.repoID, number, UpdateIssueInput{
		Title: ptrString("Renamed issue"), State: ptrString("open"),
	}); err != nil {
		t.Fatalf("reopen issue: %v", err)
	}
	if _, _, err := store.ApplyLabel(ctx, fixture.alice, fixture.repoID, number, label.ID); err != nil {
		t.Fatalf("apply label: %v", err)
	}
	if _, _, err := store.ApplyLabel(ctx, fixture.alice, fixture.repoID, number, label.ID); err != nil {
		t.Fatalf("re-apply label: %v", err)
	}
	if err := store.RemoveLabel(ctx, fixture.alice, fixture.repoID, number, label.ID); err != nil {
		t.Fatalf("remove label: %v", err)
	}
	if _, _, err := store.AssignIssueUser(
		ctx, fixture.alice, fixture.repoID, number, fixture.bob.Username,
	); err != nil {
		t.Fatalf("assign user: %v", err)
	}
	if _, _, err := store.AssignIssueUser(
		ctx, fixture.alice, fixture.repoID, number, fixture.bob.Username,
	); err != nil {
		t.Fatalf("re-assign user: %v", err)
	}
	if err := store.RemoveIssueUser(
		ctx, fixture.alice, fixture.repoID, number, fixture.bob.Username,
	); err != nil {
		t.Fatalf("remove assignee: %v", err)
	}

	events := listEvents(t, ctx, store, fixture.repoID, WorkItemIssue, number)
	assertEventKinds(t, events, []string{
		EventRetitled, EventClosed, EventReopened,
		EventLabeled, EventUnlabeled, EventAssigned, EventUnassigned,
	})
	if events[0].Payload.OldTitle != "Seed" || events[0].Payload.NewTitle != "Renamed issue" {
		t.Fatalf("retitle payload = %#v", events[0].Payload)
	}
	if events[0].Actor != fixture.alice.Username || events[0].ItemKind != WorkItemIssue {
		t.Fatalf("retitle event = %#v", events[0])
	}
	if events[3].Payload.Label == nil || events[3].Payload.Label.Name != "bug" ||
		events[3].Payload.Label.Color != "d73a4a" {
		t.Fatalf("label payload = %#v", events[3].Payload)
	}
	if events[5].Payload.Assignee == nil || events[5].Payload.Assignee.Username != fixture.bob.Username {
		t.Fatalf("assignee payload = %#v", events[5].Payload)
	}
}

func TestIntegrationPullRequestMutationsRecordTimelineEvents(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "public", "triage")
	number := seedMergeRequest(t, ctx, pool, fixture, fixture.alice.ID, "timeline-source")
	label, err := store.CreateLabel(
		ctx, fixture.alice, fixture.repoID, LabelInput{Name: "review", Color: "1f6feb"},
	)
	if err != nil {
		t.Fatalf("create label: %v", err)
	}
	milestone := seedMergeRequestMilestone(t, ctx, pool, fixture)

	if _, err := store.UpdateMergeRequest(
		ctx, fixture.alice, fixture.repoID, number,
		UpdateMergeRequestInput{Title: ptrString("Renamed request"), State: ptrString("closed")},
	); err != nil {
		t.Fatalf("close pull request: %v", err)
	}
	if _, err := store.UpdateMergeRequest(
		ctx, fixture.alice, fixture.repoID, number, UpdateMergeRequestInput{State: ptrString("open")},
	); err != nil {
		t.Fatalf("reopen pull request: %v", err)
	}
	if _, _, err := store.ApplyMergeRequestLabel(
		ctx, fixture.alice, fixture.repoID, number, label.ID,
	); err != nil {
		t.Fatalf("apply pull request label: %v", err)
	}
	if err := store.RemoveMergeRequestLabel(
		ctx, fixture.alice, fixture.repoID, number, label.ID,
	); err != nil {
		t.Fatalf("remove pull request label: %v", err)
	}
	if _, _, err := store.AssignMergeRequestUser(
		ctx, fixture.alice, fixture.repoID, number, fixture.bob.Username,
	); err != nil {
		t.Fatalf("assign pull request user: %v", err)
	}
	if err := store.RemoveMergeRequestUser(
		ctx, fixture.alice, fixture.repoID, number, fixture.bob.Username,
	); err != nil {
		t.Fatalf("remove pull request assignee: %v", err)
	}
	if _, _, err := store.SetMergeRequestMilestone(
		ctx, fixture.alice, fixture.repoID, number, &milestone.Number,
	); err != nil {
		t.Fatalf("set pull request milestone: %v", err)
	}
	if _, _, err := store.SetMergeRequestMilestone(
		ctx, fixture.alice, fixture.repoID, number, nil,
	); err != nil {
		t.Fatalf("clear pull request milestone: %v", err)
	}
	if _, _, err := store.SetMergeRequestDraft(ctx, fixture.alice, fixture.repoID, number, true); err != nil {
		t.Fatalf("convert to draft: %v", err)
	}
	if _, _, err := store.SetMergeRequestDraft(ctx, fixture.alice, fixture.repoID, number, false); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	if _, _, err := store.RequestUserReview(
		ctx, fixture.alice, fixture.repoID, number, fixture.bob.Username,
	); err != nil {
		t.Fatalf("request review: %v", err)
	}
	finalizeMergeForEvents(t, ctx, store, fixture, number)

	events := listEvents(t, ctx, store, fixture.repoID, WorkItemMergeRequest, number)
	assertEventKinds(t, events, []string{
		EventRetitled, EventClosed, EventReopened, EventLabeled, EventUnlabeled,
		EventAssigned, EventUnassigned, EventMilestoned, EventDemilestoned,
		EventDraftReady, EventReviewRequested, EventMerged,
	})
	if events[7].Payload.Milestone == nil || events[7].Payload.Milestone.Title != milestone.Title {
		t.Fatalf("milestone payload = %#v", events[7].Payload)
	}
	if events[10].Payload.Reviewer != fixture.bob.Username {
		t.Fatalf("review request payload = %#v", events[10].Payload)
	}
	if events[11].Payload.Revision != "timeline-pushed" {
		t.Fatalf("merge payload = %#v", events[11].Payload)
	}
}

func TestIntegrationWorkItemEventsPaginateAndReportMissingItems(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "public", "triage")
	number := seedIssue(t, ctx, pool, fixture, fixture.alice.ID, "open")
	for _, state := range []string{"closed", "open", "closed"} {
		if _, err := store.UpdateIssue(ctx, fixture.alice, fixture.repoID, number, UpdateIssueInput{
			State: ptrString(state),
		}); err != nil {
			t.Fatalf("update issue state %q: %v", state, err)
		}
	}
	first, err := store.ListWorkItemEvents(ctx, fixture.repoID, WorkItemIssue, number, Page{Limit: 2})
	if err != nil || len(first.Items) != 2 || !first.HasMore || first.NextCursor != "2" {
		t.Fatalf("first event page = %#v, error = %v", first, err)
	}
	last, err := store.ListWorkItemEvents(
		ctx, fixture.repoID, WorkItemIssue, number, Page{Limit: 2, Cursor: "2"},
	)
	if err != nil || len(last.Items) != 1 || last.HasMore {
		t.Fatalf("last event page = %#v, error = %v", last, err)
	}
	if _, err := store.ListWorkItemEvents(
		ctx, fixture.repoID, WorkItemIssue, number+9000, Page{},
	); err == nil || !isNotFound(err) {
		t.Fatalf("missing issue events error = %v, want not found", err)
	}
	if _, err := store.ListWorkItemEvents(
		ctx, fixture.repoID, WorkItemMergeRequest, number, Page{},
	); err == nil || !isNotFound(err) {
		t.Fatalf("wrong item kind error = %v, want not found", err)
	}
}

func finalizeMergeForEvents(
	t *testing.T,
	ctx context.Context,
	store *store,
	fixture integrationFixture,
	number int64,
) {
	t.Helper()
	operation, err := store.AcquireMergeOperation(ctx, fixture.alice.ID, fixture.repoID, number,
		"timeline-source", "main-rev", fixture.alice.ID, time.Minute)
	if err != nil {
		t.Fatalf("acquire merge operation: %v", err)
	}
	operation.State = "ready_to_push"
	operation.StagedRevision = "timeline-staged"
	operation = mustUpdateMergeOperation(t, ctx, store, operation)
	operation.State = "pushed"
	operation.PushedRevision = "timeline-pushed"
	operation = mustUpdateMergeOperation(t, ctx, store, operation)
	if _, err := store.FinalizeMerged(
		ctx, fixture.alice, fixture.repoID, number, operation.ID, "timeline-pushed",
	); err != nil {
		t.Fatalf("finalize merge: %v", err)
	}
}

func listEvents(
	t *testing.T,
	ctx context.Context,
	store *store,
	repositoryID string,
	itemKind string,
	number int64,
) []WorkItemEvent {
	t.Helper()
	result, err := store.ListWorkItemEvents(ctx, repositoryID, itemKind, number, Page{Limit: 100})
	if err != nil {
		t.Fatalf("list %s events: %v", itemKind, err)
	}
	return result.Items
}

func assertEventKinds(t *testing.T, events []WorkItemEvent, want []string) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("event kinds = %v, want %v", eventKinds(events), want)
	}
	for index, kind := range want {
		if events[index].Kind != kind {
			t.Fatalf("event kinds = %v, want %v", eventKinds(events), want)
		}
	}
}

func eventKinds(events []WorkItemEvent) []string {
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

func isNotFound(err error) bool {
	return errors.Is(err, platform.ErrNotFound)
}
