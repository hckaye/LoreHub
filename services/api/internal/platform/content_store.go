package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type loreBranchEvent struct {
	ActorID        string `json:"actorId"`
	RepositoryID   string `json:"repositoryId"`
	LoreRepository string `json:"loreRepositoryId"`
	BranchID       string `json:"branchId"`
	BranchName     string `json:"branchName"`
	LatestRevision string `json:"latestRevision,omitempty"`
}

func (store *Store) PrepareLoreBranchCreation(
	ctx context.Context,
	actorID string,
	loreRepositoryID string,
	branchID string,
	branchName string,
) error {
	if actorID == "" || loreRepositoryID == "" || branchID == "" || branchName == "" {
		return errors.New("the Lore branch creation preparation is incomplete")
	}
	command, err := store.pool.Exec(ctx, `
		INSERT INTO repository_branch_states (
			repository_id, branch_id, branch_name, latest_revision, observed_at
		)
		SELECT repositories.id, $3, $4, repeat('0', 64), now()
		FROM repositories
		JOIN organizations ON organizations.id = repositories.organization_id AND organizations.active
		JOIN users ON users.id = $2 AND users.status = 'active'
		WHERE repositories.lore_repository_id = $1
		  AND repositories.lifecycle_state = 'active'
		  AND repositories.archived_at IS NULL
		ON CONFLICT (repository_id, branch_id) DO NOTHING
	`, loreRepositoryID, actorID, branchID, branchName)
	if err != nil {
		return fmt.Errorf("prepare Lore branch creation: %w", err)
	}
	if command.RowsAffected() == 1 {
		return nil
	}
	var existingName string
	err = store.pool.QueryRow(ctx, `
		SELECT states.branch_name
		FROM repository_branch_states states
		JOIN repositories ON repositories.id = states.repository_id
		WHERE repositories.lore_repository_id = $1 AND states.branch_id = $2
	`, loreRepositoryID, branchID).Scan(&existingName)
	if err != nil || existingName != branchName {
		return errors.New("the Lore branch creation preparation conflicts with existing state")
	}
	return nil
}

func (store *Store) ObserveBranchState(
	ctx context.Context,
	repositoryID string,
	branchID string,
	branchName string,
	latestRevision string,
) error {
	if repositoryID == "" || branchID == "" || branchName == "" || latestRevision == "" {
		return errors.New("the Lore branch observation is incomplete")
	}
	_, err := store.pool.Exec(ctx, `
		INSERT INTO repository_branch_states (
			repository_id, branch_id, branch_name, latest_revision, observed_at
		) VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (repository_id, branch_id) DO UPDATE SET
			branch_name = EXCLUDED.branch_name,
			latest_revision = EXCLUDED.latest_revision,
			observed_at = EXCLUDED.observed_at
	`, repositoryID, branchID, branchName, latestRevision)
	if err != nil {
		return fmt.Errorf("record Lore branch state: %w", err)
	}
	return nil
}

func (store *Store) ObserveLoreBranchRevision(
	ctx context.Context,
	loreRepositoryID string,
	branchID string,
	revision string,
) error {
	if loreRepositoryID == "" || branchID == "" || revision == "" {
		return errors.New("the Lore branch revision observation is incomplete")
	}
	command, err := store.pool.Exec(ctx, `
		UPDATE repository_branch_states states
		SET latest_revision = $3, observed_at = now()
		FROM repositories repositories
		WHERE repositories.id = states.repository_id
		  AND repositories.lore_repository_id = $1
		  AND repositories.lifecycle_state = 'active'
		  AND states.branch_id = $2
	`, loreRepositoryID, branchID, revision)
	if err != nil {
		return fmt.Errorf("record Lore branch revision: %w", err)
	}
	if command.RowsAffected() != 1 {
		return errors.New("the Lore branch is not observed in the control plane")
	}
	return nil
}

func (store *Store) DeleteLoreBranchState(
	ctx context.Context,
	loreRepositoryID string,
	branchID string,
) error {
	if loreRepositoryID == "" || branchID == "" {
		return errors.New("the Lore branch deletion observation is incomplete")
	}
	_, err := store.pool.Exec(ctx, `
		DELETE FROM repository_branch_states states
		USING repositories repositories
		WHERE repositories.id = states.repository_id
		  AND repositories.lore_repository_id = $1
		  AND states.branch_id = $2
	`, loreRepositoryID, branchID)
	if err != nil {
		return fmt.Errorf("remove Lore branch state: %w", err)
	}
	return nil
}

func (store *Store) RecordLoreBranchCreation(
	ctx context.Context,
	actorID string,
	loreRepositoryID string,
	branchID string,
	branchName string,
	revision string,
) error {
	if actorID == "" || loreRepositoryID == "" || branchID == "" || branchName == "" || revision == "" {
		return errors.New("the Lore branch creation observation is incomplete")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin Lore branch creation observation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	repositoryID, organizationID, err := loreObservationRepository(ctx, tx, actorID, loreRepositoryID)
	if err != nil {
		return err
	}
	var deleted bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM outbox_events
			WHERE topic = 'branch.deleted' AND event_key = $1
		)
	`, branchID).Scan(&deleted); err != nil {
		return fmt.Errorf("check Lore branch deletion observation: %w", err)
	}
	if deleted {
		return nil
	}
	event := loreBranchEvent{
		ActorID: actorID, RepositoryID: repositoryID, LoreRepository: loreRepositoryID,
		BranchID: branchID, BranchName: branchName, LatestRevision: revision,
	}
	inserted, err := insertLoreObservationOutbox(ctx, tx, "branch.created", branchID, event)
	if err != nil {
		return err
	}
	if !inserted {
		return nil
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO repository_branch_states (
			repository_id, branch_id, branch_name, latest_revision, observed_at
		) VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (repository_id, branch_id) DO UPDATE SET
			branch_name = EXCLUDED.branch_name,
			latest_revision = EXCLUDED.latest_revision,
			observed_at = EXCLUDED.observed_at
		WHERE repository_branch_states.latest_revision = repeat('0', 64)
	`, repositoryID, branchID, branchName, revision)
	if err != nil {
		return fmt.Errorf("record Lore branch creation: %w", err)
	}
	if err := insertAudit(ctx, tx, actorID, organizationID, repositoryID,
		"branch.create", "lore_branch", branchID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Lore branch creation observation: %w", err)
	}
	return nil
}

func (store *Store) RecordLoreBranchPush(
	ctx context.Context,
	actorID string,
	loreRepositoryID string,
	branchID string,
	revision string,
) error {
	if actorID == "" || loreRepositoryID == "" || branchID == "" || revision == "" {
		return errors.New("the Lore branch push observation is incomplete")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin Lore branch push observation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	repositoryID, organizationID, err := loreObservationRepository(ctx, tx, actorID, loreRepositoryID)
	if err != nil {
		return err
	}
	var branchName string
	err = tx.QueryRow(ctx, `
		UPDATE repository_branch_states
		SET latest_revision = $3, observed_at = now()
		WHERE repository_id = $1 AND branch_id = $2
		RETURNING branch_name
	`, repositoryID, branchID, revision).Scan(&branchName)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("the Lore branch is not observed in the control plane")
	}
	if err != nil {
		return fmt.Errorf("record Lore branch push: %w", err)
	}
	event := loreBranchEvent{
		ActorID: actorID, RepositoryID: repositoryID, LoreRepository: loreRepositoryID,
		BranchID: branchID, BranchName: branchName, LatestRevision: revision,
	}
	eventKey := branchID + ":" + revision
	inserted, err := insertLoreObservationOutbox(ctx, tx, "branch.pushed", eventKey, event)
	if err != nil {
		return err
	}
	if inserted {
		if err := insertAudit(ctx, tx, actorID, organizationID, repositoryID,
			"branch.push", "lore_branch", branchID); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Lore branch push observation: %w", err)
	}
	return nil
}

func (store *Store) RecordLoreBranchDeletion(
	ctx context.Context,
	actorID string,
	loreRepositoryID string,
	branchID string,
	branchName string,
	revision string,
) error {
	if actorID == "" || loreRepositoryID == "" || branchID == "" || branchName == "" || revision == "" {
		return errors.New("the Lore branch deletion observation is incomplete")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin Lore branch deletion observation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	repositoryID, organizationID, err := loreObservationRepository(ctx, tx, actorID, loreRepositoryID)
	if err != nil {
		return err
	}
	var storedName, storedRevision string
	err = tx.QueryRow(ctx, `
		DELETE FROM repository_branch_states
		WHERE repository_id = $1 AND branch_id = $2
		RETURNING branch_name, latest_revision
	`, repositoryID, branchID).Scan(&storedName, &storedRevision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("record Lore branch deletion: %w", err)
	}
	if err == nil && (storedName != branchName || storedRevision != revision) {
		return errors.New("the Lore branch deletion observation conflicts with existing state")
	}
	event := loreBranchEvent{
		ActorID: actorID, RepositoryID: repositoryID, LoreRepository: loreRepositoryID,
		BranchID: branchID, BranchName: branchName, LatestRevision: revision,
	}
	inserted, err := insertLoreObservationOutbox(ctx, tx, "branch.deleted", branchID, event)
	if err != nil {
		return err
	}
	if inserted {
		if err := insertAudit(ctx, tx, actorID, organizationID, repositoryID,
			"branch.delete", "lore_branch", branchID); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Lore branch deletion observation: %w", err)
	}
	return nil
}

func loreObservationRepository(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	loreRepositoryID string,
) (string, string, error) {
	var repositoryID, organizationID string
	err := tx.QueryRow(ctx, `
		SELECT repositories.id, repositories.organization_id
		FROM repositories
		JOIN organizations ON organizations.id = repositories.organization_id AND organizations.active
		JOIN users ON users.id = $2 AND users.status = 'active'
		WHERE repositories.lore_repository_id = $1
		  AND repositories.lifecycle_state = 'active'
		  AND repositories.archived_at IS NULL
	`, loreRepositoryID, actorID).Scan(&repositoryID, &organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrForbidden
	}
	if err != nil {
		return "", "", fmt.Errorf("resolve Lore observation repository: %w", err)
	}
	return repositoryID, organizationID, nil
}

func insertLoreObservationOutbox(
	ctx context.Context,
	tx pgx.Tx,
	topic string,
	eventKey string,
	payload loreBranchEvent,
) (bool, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("encode Lore branch observation: %w", err)
	}
	var insertedID string
	err = tx.QueryRow(ctx, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (topic, event_key) DO NOTHING
		RETURNING id
	`, uuid.New(), topic, strings.TrimSpace(eventKey), encoded).Scan(&insertedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("record Lore branch outbox event: %w", err)
	}
	return true, nil
}

func (store *Store) ListIssuesForRead(
	ctx context.Context,
	actor *User,
	repository Repository,
	state string,
) ([]Issue, error) {
	if _, err := store.RepositoryForRead(ctx, actor, repository.Owner, repository.Slug); err != nil {
		return nil, err
	}
	if state != "open" && state != "closed" {
		state = "open"
	}
	rows, err := store.pool.Query(ctx, `
		SELECT i.id, i.number, i.title, i.body, i.state, author.username,
		       assignee.username, COUNT(c.id), i.created_at, i.updated_at
		FROM issues i
		JOIN users author ON author.id = i.author_id
		LEFT JOIN users assignee ON assignee.id = i.assignee_id
		LEFT JOIN issue_comments c ON c.issue_id = i.id
		WHERE i.repository_id = $1 AND i.state = $2
		GROUP BY i.id, author.username, assignee.username
		ORDER BY i.updated_at DESC
		LIMIT 100
	`, repository.ID, state)
	if err != nil {
		return nil, fmt.Errorf("list authorized issues: %w", err)
	}
	defer rows.Close()
	issues := make([]Issue, 0)
	for rows.Next() {
		var issue Issue
		if err := rows.Scan(&issue.ID, &issue.Number, &issue.Title, &issue.Body, &issue.State, &issue.Author,
			&issue.Assignee, &issue.CommentCount, &issue.CreatedAt, &issue.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan authorized issue: %w", err)
		}
		issues = append(issues, issue)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate authorized issues: %w", err)
	}
	return issues, nil
}

func (store *Store) ListMergeRequestsForRead(
	ctx context.Context,
	actor *User,
	repository Repository,
	state string,
) ([]MergeRequest, error) {
	if _, err := store.RepositoryForRead(ctx, actor, repository.Owner, repository.Slug); err != nil {
		return nil, err
	}
	if state != "open" && state != "closed" && state != "merged" {
		state = "open"
	}
	rows, err := store.pool.Query(ctx, `
		SELECT mr.id, mr.number, mr.title, mr.body, mr.state,
		       mr.source_branch, mr.target_branch, mr.source_revision, mr.target_revision,
		       author.username,
		       COUNT(review.id) FILTER (WHERE review.decision = 'approved'),
		       mr.created_at, mr.updated_at
		FROM merge_requests mr
		JOIN users author ON author.id = mr.author_id
		LEFT JOIN merge_request_reviews review ON review.merge_request_id = mr.id
		WHERE mr.repository_id = $1 AND mr.state = $2
		GROUP BY mr.id, author.username
		ORDER BY mr.updated_at DESC
		LIMIT 100
	`, repository.ID, state)
	if err != nil {
		return nil, fmt.Errorf("list authorized merge requests: %w", err)
	}
	defer rows.Close()
	mergeRequests := make([]MergeRequest, 0)
	for rows.Next() {
		var mergeRequest MergeRequest
		if err := rows.Scan(
			&mergeRequest.ID,
			&mergeRequest.Number,
			&mergeRequest.Title,
			&mergeRequest.Body,
			&mergeRequest.State,
			&mergeRequest.SourceBranch,
			&mergeRequest.TargetBranch,
			&mergeRequest.SourceRevision,
			&mergeRequest.TargetRevision,
			&mergeRequest.Author,
			&mergeRequest.ApprovalCount,
			&mergeRequest.CreatedAt,
			&mergeRequest.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan authorized merge request: %w", err)
		}
		mergeRequests = append(mergeRequests, mergeRequest)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate authorized merge requests: %w", err)
	}
	return mergeRequests, nil
}

func (store *Store) ListCIRunsForRead(
	ctx context.Context,
	actor *User,
	repository Repository,
) ([]CIRun, error) {
	if _, err := store.RepositoryForRead(ctx, actor, repository.Owner, repository.Slug); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT run.id, run.run_number, run.event_name, run.branch, run.revision,
		       run.status, run.conclusion, run.queued_at, run.started_at, run.completed_at
		FROM ci_runs run
		WHERE run.repository_id = $1
		ORDER BY run.run_number DESC
		LIMIT 50
	`, repository.ID)
	if err != nil {
		return nil, fmt.Errorf("list authorized CI runs: %w", err)
	}
	defer rows.Close()
	runs := make([]CIRun, 0)
	for rows.Next() {
		var run CIRun
		if err := rows.Scan(
			&run.ID,
			&run.RunNumber,
			&run.EventName,
			&run.Branch,
			&run.Revision,
			&run.Status,
			&run.Conclusion,
			&run.QueuedAt,
			&run.StartedAt,
			&run.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan authorized CI run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate authorized CI runs: %w", err)
	}
	return runs, nil
}
