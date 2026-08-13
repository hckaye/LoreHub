package collab

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Work item kinds stored in work_item_events.item_kind.
const (
	WorkItemIssue        = "issue"
	WorkItemMergeRequest = "merge_request"
)

// Timeline event kinds stored in work_item_events.event_kind.
const (
	EventClosed          = "closed"
	EventReopened        = "reopened"
	EventLabeled         = "labeled"
	EventUnlabeled       = "unlabeled"
	EventAssigned        = "assigned"
	EventUnassigned      = "unassigned"
	EventMilestoned      = "milestoned"
	EventDemilestoned    = "demilestoned"
	EventRetitled        = "retitled"
	EventMerged          = "merged"
	EventReviewRequested = "review_requested"
	EventDraftReady      = "draft_ready"
)

// WorkItemEvent is one entry of an issue or pull request timeline. It records
// what changed, who changed it and when, so the conversation view can render
// state changes between comments.
type WorkItemEvent struct {
	ID        string               `json:"id"`
	ItemKind  string               `json:"itemKind"`
	ItemID    string               `json:"itemId"`
	Actor     string               `json:"actor"`
	Kind      string               `json:"kind"`
	Payload   WorkItemEventPayload `json:"payload"`
	CreatedAt time.Time            `json:"createdAt"`
}

// WorkItemEventPayload carries the details a timeline row needs to render.
// Every field is optional; only the ones relevant to the event kind are set.
type WorkItemEventPayload struct {
	Label     *Label            `json:"label,omitempty"`
	Assignee  *Assignee         `json:"assignee,omitempty"`
	Milestone *MilestoneSummary `json:"milestone,omitempty"`
	OldTitle  string            `json:"oldTitle,omitempty"`
	NewTitle  string            `json:"newTitle,omitempty"`
	Reviewer  string            `json:"reviewer,omitempty"`
	Revision  string            `json:"revision,omitempty"`
}

// WorkItemEventRecord is the input for appending one timeline event.
type WorkItemEventRecord struct {
	RepositoryID string
	ItemKind     string
	ItemID       string
	ActorID      string
	Kind         string
	Payload      WorkItemEventPayload
}

// RecordWorkItemEvent appends a timeline event inside the caller's transaction
// so the event and the mutation it describes commit together. The actor name is
// resolved from the user id to keep the event readable after membership changes.
func RecordWorkItemEvent(ctx context.Context, tx pgx.Tx, record WorkItemEventRecord) error {
	payload, err := encodeJSON(record.Payload)
	if err != nil {
		return fmt.Errorf("encode work item event payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO work_item_events (
			id, repository_id, item_kind, item_id, actor, event_kind, payload, created_at
		) VALUES (
			$1, $2, $3, $4, (SELECT username FROM users WHERE id = $5), $6, $7, $8
		)
	`, uuidArg(), record.RepositoryID, record.ItemKind, record.ItemID,
		record.ActorID, record.Kind, payload, nowUTC())
	if err != nil {
		return fmt.Errorf("record work item event: %w", err)
	}
	return nil
}

// recordMergeRequestMetadataEvent translates a pull request label, assignee or
// milestone change into the matching timeline event.
func recordMergeRequestMetadataEvent(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	request mergeRequestMetadataRef,
	change string,
	subject any,
) error {
	record := WorkItemEventRecord{
		RepositoryID: request.RepositoryID, ItemKind: WorkItemMergeRequest,
		ItemID: request.ID, ActorID: actorID,
	}
	switch value := subject.(type) {
	case Label:
		record.Kind = EventLabeled
		if change == "label_removed" {
			record.Kind = EventUnlabeled
		}
		record.Payload.Label = &value
	case Assignee:
		record.Kind = EventAssigned
		if change == "assignee_removed" {
			record.Kind = EventUnassigned
		}
		record.Payload.Assignee = &value
	case *MilestoneSummary:
		record.Kind = EventMilestoned
		if value == nil {
			record.Kind = EventDemilestoned
		}
		record.Payload.Milestone = value
	default:
		return nil
	}
	return RecordWorkItemEvent(ctx, tx, record)
}

// recordStateChangeEvents appends the retitled and closed/reopened events implied
// by a partial issue or pull request update. Unchanged fields record nothing.
func recordStateChangeEvents(
	ctx context.Context,
	tx pgx.Tx,
	record WorkItemEventRecord,
	previousTitle string,
	previousState string,
	title *string,
	state *string,
) error {
	if title != nil && *title != previousTitle {
		titled := record
		titled.Kind = EventRetitled
		titled.Payload = WorkItemEventPayload{OldTitle: previousTitle, NewTitle: *title}
		if err := RecordWorkItemEvent(ctx, tx, titled); err != nil {
			return err
		}
	}
	if state == nil || *state == previousState {
		return nil
	}
	changed := record
	changed.Kind = EventReopened
	if *state == "closed" {
		changed.Kind = EventClosed
	}
	return RecordWorkItemEvent(ctx, tx, changed)
}
