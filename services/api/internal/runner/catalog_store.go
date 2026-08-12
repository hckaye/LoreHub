package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (store *Store) Repositories(ctx context.Context) ([]Repository, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT r.id, o.slug, r.slug, r.lore_url, r.default_branch
		FROM repositories r
		JOIN organizations o ON o.id = r.organization_id
		WHERE r.archived_at IS NULL
		ORDER BY r.id
	`)
	if err != nil {
		return nil, fmt.Errorf("list repositories for branch polling: %w", err)
	}
	defer rows.Close()
	repositories := make([]Repository, 0)
	for rows.Next() {
		var repository Repository
		if err := rows.Scan(&repository.ID, &repository.Owner, &repository.Slug, &repository.LoreURL,
			&repository.DefaultBranch); err != nil {
			return nil, fmt.Errorf("scan repository for branch polling: %w", err)
		}
		repositories = append(repositories, repository)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repositories for branch polling: %w", err)
	}
	return repositories, nil
}

func (store *Store) LatestBranchRevision(ctx context.Context, repositoryID string, branch string) (string, error) {
	var revision string
	err := store.pool.QueryRow(ctx, `
		SELECT latest_revision
		FROM repository_branch_states
		WHERE repository_id = $1 AND branch_name = $2
		ORDER BY observed_at DESC
		LIMIT 1
	`, repositoryID, branch).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrActionNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read observed Lore branch revision: %w", err)
	}
	return revision, nil
}

func (store *Store) ObserveBranch(
	ctx context.Context,
	repository Repository,
	branch ObservedBranch,
	workflows ...WorkflowDefinition,
) (bool, error) {
	canonical := branch.Name == repository.DefaultBranch
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin branch observation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	var previous string
	var workflowRevision *string
	err = transaction.QueryRow(ctx, `
		SELECT latest_revision, workflow_observed_revision
		FROM repository_branch_states
		WHERE repository_id = $1 AND branch_id = $2
		FOR UPDATE
	`, repository.ID, branch.ID).Scan(&previous, &workflowRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		var observedWorkflowRevision *string
		if workflows != nil || isZeroLoreRevision(branch.LatestRevision) {
			observedWorkflowRevision = &branch.LatestRevision
		}
		_, err = transaction.Exec(ctx, `
			INSERT INTO repository_branch_states (
				repository_id, branch_id, branch_name, latest_revision,
				workflow_observed_revision, observed_at
			) VALUES ($1, $2, $3, $4, $5, now())
		`, repository.ID, branch.ID, branch.Name, branch.LatestRevision, observedWorkflowRevision)
		if err != nil {
			return false, fmt.Errorf("record initial branch state: %w", err)
		}
		if workflows != nil {
			if canonical {
				if err := store.syncWorkflows(ctx, transaction, repository, branch.LatestRevision, workflows); err != nil {
					return false, err
				}
			} else if _, err := store.syncWorkflowRevisions(
				ctx, transaction, repository, branch.LatestRevision, workflows,
			); err != nil {
				return false, err
			}
		}
		if err := transaction.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit initial branch state: %w", err)
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read previous branch state: %w", err)
	}
	catalogChanged := workflowRevision == nil || *workflowRevision != branch.LatestRevision
	if previous == branch.LatestRevision && !catalogChanged {
		_, err = transaction.Exec(ctx, `
			UPDATE repository_branch_states
			SET branch_name = $3, observed_at = now()
			WHERE repository_id = $1 AND branch_id = $2
		`, repository.ID, branch.ID, branch.Name)
		if err != nil {
			return false, fmt.Errorf("refresh branch observation: %w", err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit branch refresh: %w", err)
		}
		return false, nil
	}

	if workflows == nil && !isZeroLoreRevision(branch.LatestRevision) {
		_, err = transaction.Exec(ctx, `
			UPDATE repository_branch_states
			SET branch_name = $3, latest_revision = $4, observed_at = now()
			WHERE repository_id = $1 AND branch_id = $2
		`, repository.ID, branch.ID, branch.Name, branch.LatestRevision)
		if err != nil {
			return false, fmt.Errorf("update branch state without workflow catalog: %w", err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit branch state without workflow catalog: %w", err)
		}
		return false, nil
	}

	_, err = transaction.Exec(ctx, `
		UPDATE repository_branch_states
		SET branch_name = $3, latest_revision = $4,
			workflow_observed_revision = $4, observed_at = now()
		WHERE repository_id = $1 AND branch_id = $2
	`, repository.ID, branch.ID, branch.Name, branch.LatestRevision)
	if err != nil {
		return false, fmt.Errorf("update branch state: %w", err)
	}
	queued := false
	if workflows != nil && catalogChanged {
		var revisionWorkflowIDs map[string]uuid.UUID
		if canonical {
			if err := store.syncWorkflows(ctx, transaction, repository, branch.LatestRevision, workflows); err != nil {
				return false, err
			}
		} else {
			revisionWorkflowIDs, err = store.syncWorkflowRevisions(
				ctx, transaction, repository, branch.LatestRevision, workflows,
			)
			if err != nil {
				return false, err
			}
		}
		if workflowRevision != nil {
			if err := store.enqueuePushes(
				ctx, transaction, repository, branch, *workflowRevision, workflows,
				canonical, revisionWorkflowIDs,
			); err != nil {
				return false, err
			}
			queued = true
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit branch observation: %w", err)
	}
	return queued, nil
}

func (store *Store) syncWorkflowRevisions(
	ctx context.Context,
	transaction pgx.Tx,
	repository Repository,
	revision string,
	workflows []WorkflowDefinition,
) (map[string]uuid.UUID, error) {
	workflowIDs := make(map[string]uuid.UUID, len(workflows))
	for _, workflow := range workflows {
		if workflow.Path == "" {
			continue
		}
		state := workflow.State
		if state == "" {
			state = "active"
			if !workflow.Enabled {
				state = "disabled"
			}
		}
		triggerConfig := workflow.TriggerConfig
		if len(triggerConfig) == 0 {
			triggerConfig = json.RawMessage(`{}`)
		}
		id := uuid.New()
		err := transaction.QueryRow(ctx, `
			INSERT INTO ci_workflow_revisions (
				id, repository_id, revision, path, name, enabled, state, error_code, error_message, trigger_config
			) VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''), $10)
			ON CONFLICT (repository_id, revision, path) DO UPDATE SET
				name = EXCLUDED.name,
				enabled = EXCLUDED.enabled,
				state = EXCLUDED.state,
				error_code = EXCLUDED.error_code,
				error_message = EXCLUDED.error_message,
				trigger_config = EXCLUDED.trigger_config
			RETURNING id
		`, id, repository.ID, revision, workflow.Path, workflow.Name, workflow.Enabled, state,
			workflow.ErrorCode, workflow.ErrorMessage, triggerConfig).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("upsert Lore workflow revision %q: %w", workflow.Path, err)
		}
		workflowIDs[workflow.Path] = id
	}
	return workflowIDs, nil
}

func (store *Store) syncWorkflows(
	ctx context.Context,
	transaction pgx.Tx,
	repository Repository,
	revision string,
	workflows []WorkflowDefinition,
) error {
	seen := make([]string, 0, len(workflows))
	for _, workflow := range workflows {
		if workflow.Path == "" {
			continue
		}
		state := workflow.State
		if state == "" {
			state = "active"
			if !workflow.Enabled {
				state = "disabled"
			}
		}
		triggerConfig := workflow.TriggerConfig
		if len(triggerConfig) == 0 {
			triggerConfig = json.RawMessage(`{}`)
		}
		seen = append(seen, workflow.Path)
		_, err := transaction.Exec(ctx, `
			INSERT INTO ci_workflows (
				id, repository_id, path, name, enabled, state, error_code, error_message,
				last_seen_revision, trigger_config
			) VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''), $9, $10)
			ON CONFLICT (repository_id, path) DO UPDATE SET
				name = EXCLUDED.name,
				enabled = EXCLUDED.enabled,
				state = EXCLUDED.state,
				error_code = EXCLUDED.error_code,
				error_message = EXCLUDED.error_message,
				last_seen_revision = EXCLUDED.last_seen_revision,
				trigger_config = EXCLUDED.trigger_config,
				updated_at = now()
		`, uuid.New(), repository.ID, workflow.Path, workflow.Name, workflow.Enabled, state,
			workflow.ErrorCode, workflow.ErrorMessage, revision, triggerConfig)
		if err != nil {
			return fmt.Errorf("upsert CI workflow %q: %w", workflow.Path, err)
		}
	}
	_, err := transaction.Exec(ctx, `
		UPDATE ci_workflows
		SET enabled = false, state = 'disabled', error_code = 'workflow_removed',
			error_message = 'Workflow is not present at the observed Lore revision',
			last_seen_revision = $2, updated_at = now()
		WHERE repository_id = $1
		  AND NOT (path = ANY($3::text[]))
	`, repository.ID, revision, seen)
	if err != nil {
		return fmt.Errorf("disable removed CI workflows: %w", err)
	}
	return nil
}

func (store *Store) enqueuePushes(
	ctx context.Context,
	transaction pgx.Tx,
	repository Repository,
	branch ObservedBranch,
	previousRevision string,
	workflows []WorkflowDefinition,
	canonical bool,
	revisionWorkflowIDs map[string]uuid.UUID,
) error {
	payload, err := json.Marshal(map[string]any{
		"ref":    "refs/heads/" + branch.Name,
		"before": previousRevision,
		"after":  branch.LatestRevision,
		"repository": map[string]string{
			"name":      repository.Slug,
			"full_name": repository.Owner + "/" + repository.Slug,
		},
	})
	if err != nil {
		return fmt.Errorf("encode CI event: %w", err)
	}
	for _, workflow := range workflows {
		if !workflow.MatchesPush(branch.Name) {
			continue
		}
		var workflowID string
		var revisionWorkflowID *uuid.UUID
		if canonical {
			if err := transaction.QueryRow(ctx, `
				SELECT id FROM ci_workflows WHERE repository_id = $1 AND path = $2
			`, repository.ID, workflow.Path).Scan(&workflowID); err != nil {
				return fmt.Errorf("find CI workflow %q: %w", workflow.Path, err)
			}
		} else {
			id, ok := revisionWorkflowIDs[workflow.Path]
			if !ok {
				return fmt.Errorf("find Lore workflow revision %q", workflow.Path)
			}
			revisionWorkflowID = &id
		}
		if _, err := store.enqueueRun(ctx, transaction, repository, workflowID, revisionWorkflowID,
			workflow.Name, workflow.Path, "push", branch.Name, branch.LatestRevision, payload,
			workflow.Environment, "", nil); err != nil {
			return err
		}
	}
	return nil
}

type PullRequestEvent struct {
	Action         string `json:"action"`
	Number         int64  `json:"number"`
	SourceBranch   string `json:"source_branch"`
	TargetBranch   string `json:"target_branch"`
	SourceRevision string `json:"source_revision"`
	TargetRevision string `json:"target_revision"`
}

type RepositoryDispatchEvent struct {
	EventType     string          `json:"event_type"`
	Branch        string          `json:"branch"`
	Revision      string          `json:"revision,omitempty"`
	ClientPayload json.RawMessage `json:"client_payload"`
}
