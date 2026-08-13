package milestones

import (
	"context"
	"testing"
)

func TestPostgresIssueMilestoneChangesRecordTimelineEvents(t *testing.T) {
	fixture := openMilestoneFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	milestone, err := store.Create(ctx, fixture.writer, fixture.repo, CreateInput{Title: "Version 3"})
	if err != nil {
		t.Fatalf("create milestone: %v", err)
	}
	if _, err := store.AssignIssue(ctx, fixture.triager, fixture.repo, 1, milestone.Number); err != nil {
		t.Fatalf("assign issue milestone: %v", err)
	}
	if _, err := store.AssignIssue(ctx, fixture.triager, fixture.repo, 1, milestone.Number); err != nil {
		t.Fatalf("repeat issue milestone assignment: %v", err)
	}
	if err := store.RemoveIssue(ctx, fixture.triager, fixture.repo, 1); err != nil {
		t.Fatalf("remove issue milestone: %v", err)
	}
	if err := store.RemoveIssue(ctx, fixture.triager, fixture.repo, 1); err != nil {
		t.Fatalf("repeat issue milestone removal: %v", err)
	}

	rows, err := fixture.pool.Query(ctx, `
		SELECT event_kind, actor, payload ->> 'milestone'
		FROM work_item_events
		WHERE repository_id = $1 AND item_kind = 'issue' AND item_id = $2
		ORDER BY created_at, id
	`, fixture.repoID, fixture.issueID)
	if err != nil {
		t.Fatalf("query timeline events: %v", err)
	}
	defer rows.Close()
	kinds := make([]string, 0, 2)
	for rows.Next() {
		var kind, actor string
		var milestonePayload *string
		if err := rows.Scan(&kind, &actor, &milestonePayload); err != nil {
			t.Fatalf("scan timeline event: %v", err)
		}
		if actor != fixture.triager.Username {
			t.Fatalf("event actor = %q, want %q", actor, fixture.triager.Username)
		}
		if kind == "milestoned" && (milestonePayload == nil || *milestonePayload == "") {
			t.Fatal("milestoned event has no milestone payload")
		}
		if kind == "demilestoned" && milestonePayload != nil {
			t.Fatalf("demilestoned event payload = %q, want none", *milestonePayload)
		}
		kinds = append(kinds, kind)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate timeline events: %v", err)
	}
	if len(kinds) != 2 || kinds[0] != "milestoned" || kinds[1] != "demilestoned" {
		t.Fatalf("timeline event kinds = %v, want [milestoned demilestoned]", kinds)
	}
}
