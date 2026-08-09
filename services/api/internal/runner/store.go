package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	ID            string
	Owner         string
	Slug          string
	LoreURL       string
	DefaultBranch string
}

type ObservedBranch struct {
	ID             string
	Name           string
	LatestRevision string
}

type Job struct {
	ID           string
	Attempt      int
	RunID        string
	WorkflowPath string
	RepositoryID string
	Owner        string
	Repository   string
	LoreURL      string
	Revision     string
	Branch       string
	EventName    string
	EventPayload json.RawMessage
}

type Artifact struct {
	Name      string
	ObjectKey string
	Size      int64
}

type Store struct {
	pool              *pgxpool.Pool
	logDirectory      string
	artifactDirectory string
}

func NewStore(pool *pgxpool.Pool) *Store {
	return NewStoreWithFiles(pool, ".cache/lorehub/runner-logs", ".cache/lorehub/runner-artifacts")
}

func NewStoreWithFiles(pool *pgxpool.Pool, logDirectory string, artifactDirectory string) *Store {
	return &Store{
		pool:              pool,
		logDirectory:      logDirectory,
		artifactDirectory: artifactDirectory,
	}
}

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
	err = transaction.QueryRow(ctx, `
		SELECT latest_revision
		FROM repository_branch_states
		WHERE repository_id = $1 AND branch_id = $2
		FOR UPDATE
	`, repository.ID, branch.ID).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = transaction.Exec(ctx, `
			INSERT INTO repository_branch_states (
				repository_id, branch_id, branch_name, latest_revision, observed_at
			) VALUES ($1, $2, $3, $4, now())
		`, repository.ID, branch.ID, branch.Name, branch.LatestRevision)
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
	if previous == branch.LatestRevision {
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

	_, err = transaction.Exec(ctx, `
		UPDATE repository_branch_states
		SET branch_name = $3, latest_revision = $4, observed_at = now()
		WHERE repository_id = $1 AND branch_id = $2
	`, repository.ID, branch.ID, branch.Name, branch.LatestRevision)
	if err != nil {
		return false, fmt.Errorf("update branch state: %w", err)
	}
	if workflows != nil {
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
		if err := store.enqueuePushes(
			ctx, transaction, repository, branch, previous, workflows, canonical, revisionWorkflowIDs,
		); err != nil {
			return false, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit branch observation: %w", err)
	}
	return true, nil
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
			workflow.Name, workflow.Path, "push", branch.Name, branch.LatestRevision, payload, "", nil); err != nil {
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

func (store *Store) EnqueueScheduledRuns(
	ctx context.Context,
	repository Repository,
	revision string,
	now time.Time,
) ([]RunRecord, error) {
	if repository.DefaultBranch == "" || revision == "" {
		return nil, ErrActionInvalid
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin scheduled Actions runs: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	rows, err := transaction.Query(ctx, `
		SELECT id, path, name, enabled, state, trigger_config
		FROM ci_workflows
		WHERE repository_id = $1
		ORDER BY path
		FOR UPDATE
	`, repository.ID)
	if err != nil {
		return nil, fmt.Errorf("list scheduled Actions workflows: %w", err)
	}
	type workflowRow struct {
		id, path, name, state string
		enabled               bool
		triggerConfig         json.RawMessage
	}
	workflowRows := make([]workflowRow, 0)
	for rows.Next() {
		var workflow workflowRow
		if err := rows.Scan(
			&workflow.id, &workflow.path, &workflow.name, &workflow.enabled, &workflow.state,
			&workflow.triggerConfig,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan scheduled Actions workflow: %w", err)
		}
		workflowRows = append(workflowRows, workflow)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate scheduled Actions workflows: %w", err)
	}
	rows.Close()
	runIDs := make([]uuid.UUID, 0)
	for _, row := range workflowRows {
		definition, err := workflowFromTriggerConfig(
			row.path, row.name, row.enabled, row.state, row.triggerConfig,
		)
		if err != nil {
			return nil, err
		}
		for _, occurrence := range definition.ScheduleOccurrences(now) {
			key := repository.ID + ":" + row.id + ":" + occurrence.Key + ":" + occurrence.At.Format(time.RFC3339)
			if _, err := transaction.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
				return nil, fmt.Errorf("lock scheduled Actions occurrence: %w", err)
			}
			var alreadyQueued bool
			if err := transaction.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM ci_schedule_occurrences
					WHERE workflow_id = $1 AND schedule_key = $2 AND occurrence_at = $3
				)
			`, row.id, occurrence.Key, occurrence.At).Scan(&alreadyQueued); err != nil {
				return nil, fmt.Errorf("check scheduled Actions occurrence: %w", err)
			}
			if alreadyQueued {
				continue
			}
			payload, err := json.Marshal(map[string]any{
				"ref":          "refs/heads/" + repository.DefaultBranch,
				"after":        revision,
				"schedule":     occurrence.Key,
				"scheduled_at": occurrence.At.Format(time.RFC3339),
				"repository": map[string]string{
					"name": repository.Slug, "full_name": repository.Owner + "/" + repository.Slug,
				},
			})
			if err != nil {
				return nil, fmt.Errorf("encode scheduled Actions event: %w", err)
			}
			runID, err := store.enqueueRun(
				ctx, transaction, repository, row.id, nil, row.name, row.path, "schedule",
				repository.DefaultBranch, revision, payload, "", nil,
			)
			if err != nil {
				return nil, err
			}
			if _, err := transaction.Exec(ctx, `
				INSERT INTO ci_schedule_occurrences (workflow_id, schedule_key, occurrence_at, run_id)
				VALUES ($1, $2, $3, $4)
			`, row.id, occurrence.Key, occurrence.At, runID); err != nil {
				return nil, fmt.Errorf("record scheduled Actions occurrence: %w", err)
			}
			runIDs = append(runIDs, runID)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit scheduled Actions runs: %w", err)
	}
	return store.actionRunsByIDs(ctx, runIDs)
}

func (store *Store) DispatchRepositoryEvent(
	ctx context.Context,
	access RepositoryAccess,
	event RepositoryDispatchEvent,
	actorID string,
) ([]RunRecord, error) {
	if !access.CanWrite || strings.TrimSpace(event.EventType) == "" || len(event.EventType) > 100 ||
		event.Branch == "" || event.Revision == "" {
		return nil, ErrActionInvalid
	}
	if len(event.ClientPayload) == 0 {
		event.ClientPayload = json.RawMessage(`{}`)
	}
	if !json.Valid(event.ClientPayload) {
		return nil, ErrActionInvalid
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin repository dispatch: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	runIDs, err := store.enqueueCanonicalEvent(
		ctx,
		transaction,
		access,
		"repository_dispatch",
		event.Branch,
		event.Revision,
		actorID,
		func(definition WorkflowDefinition) bool {
			return definition.MatchesRepositoryDispatch(event.EventType)
		},
		func(repository Repository) ([]byte, error) {
			return json.Marshal(map[string]any{
				"event_type":     event.EventType,
				"client_payload": json.RawMessage(event.ClientPayload),
				"ref":            "refs/heads/" + event.Branch,
				"after":          event.Revision,
				"repository": map[string]string{
					"name": repository.Slug, "full_name": repository.Owner + "/" + repository.Slug,
				},
			})
		},
	)
	if err != nil {
		return nil, err
	}
	for _, runID := range runIDs {
		if err := recordActionEvent(ctx, transaction, access, actorID, "actions.repository_dispatch",
			runID.String(), map[string]any{"event_type": event.EventType}); err != nil {
			return nil, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit repository dispatch: %w", err)
	}
	return store.actionRunsByIDs(ctx, runIDs)
}

func (store *Store) EnqueuePullRequest(
	ctx context.Context,
	access RepositoryAccess,
	event PullRequestEvent,
	actorID string,
) ([]RunRecord, error) {
	if !access.CanWrite || event.Action == "" || event.SourceBranch == "" || event.TargetBranch == "" ||
		event.SourceRevision == "" || event.TargetRevision == "" || event.Number < 1 {
		return nil, ErrActionInvalid
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin pull request Actions event: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	payload, err := json.Marshal(map[string]any{
		"action": event.Action,
		"number": event.Number,
		"ref":    fmt.Sprintf("refs/pull/%d/merge", event.Number),
		"after":  event.SourceRevision,
		"pull_request": map[string]any{
			"head": map[string]string{"ref": event.SourceBranch, "sha": event.SourceRevision},
			"base": map[string]string{"ref": event.TargetBranch, "sha": event.TargetRevision},
		},
		"repository": map[string]string{
			"name": access.Slug, "full_name": access.Owner + "/" + access.Slug,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode pull request Actions event: %w", err)
	}
	runIDs, err := store.enqueueCanonicalEvent(
		ctx,
		transaction,
		access,
		"pull_request",
		event.TargetBranch,
		event.SourceRevision,
		actorID,
		func(definition WorkflowDefinition) bool {
			return definition.MatchesPullRequest(event.TargetBranch, event.Action)
		},
		func(Repository) ([]byte, error) { return payload, nil },
	)
	if err != nil {
		return nil, err
	}
	for _, runID := range runIDs {
		if err := recordActionEvent(ctx, transaction, access, actorID, "actions.pull_request",
			runID.String(), map[string]any{"action": event.Action, "number": event.Number}); err != nil {
			return nil, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit pull request Actions event: %w", err)
	}
	return store.actionRunsByIDs(ctx, runIDs)
}

func (store *Store) enqueueCanonicalEvent(
	ctx context.Context,
	transaction pgx.Tx,
	access RepositoryAccess,
	eventName string,
	branch string,
	revision string,
	actorID string,
	matches func(WorkflowDefinition) bool,
	encode func(Repository) ([]byte, error),
) ([]uuid.UUID, error) {
	rows, err := transaction.Query(ctx, `
		SELECT id, path, name, enabled, state, trigger_config
		FROM ci_workflows WHERE repository_id = $1 ORDER BY path FOR UPDATE
	`, access.ID)
	if err != nil {
		return nil, fmt.Errorf("list canonical Actions workflows: %w", err)
	}
	type workflowRow struct {
		id, path, name, state string
		enabled               bool
		triggerConfig         json.RawMessage
	}
	workflowRows := make([]workflowRow, 0)
	for rows.Next() {
		var workflow workflowRow
		if err := rows.Scan(&workflow.id, &workflow.path, &workflow.name, &workflow.enabled,
			&workflow.state, &workflow.triggerConfig); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan canonical Actions workflow: %w", err)
		}
		workflowRows = append(workflowRows, workflow)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate canonical Actions workflows: %w", err)
	}
	rows.Close()
	repository := Repository{
		ID: access.ID, Owner: access.Owner, Slug: access.Slug, LoreURL: access.LoreURL,
		DefaultBranch: access.DefaultBranch,
	}
	runIDs := make([]uuid.UUID, 0)
	for _, workflow := range workflowRows {
		definition, err := workflowFromTriggerConfig(
			workflow.path, workflow.name, workflow.enabled, workflow.state, workflow.triggerConfig,
		)
		if err != nil {
			return nil, err
		}
		if !matches(definition) {
			continue
		}
		payload, err := encode(repository)
		if err != nil {
			return nil, err
		}
		runID, err := store.enqueueRun(
			ctx, transaction, repository, workflow.id, nil, workflow.name, workflow.path, eventName,
			branch, revision, payload, actorID, nil,
		)
		if err != nil {
			return nil, err
		}
		runIDs = append(runIDs, runID)
	}
	return runIDs, nil
}

func (store *Store) enqueueRun(
	ctx context.Context,
	transaction pgx.Tx,
	repository Repository,
	workflowID string,
	workflowRevisionID *uuid.UUID,
	workflowName string,
	workflowPath string,
	eventName string,
	branch string,
	revision string,
	payload []byte,
	actorID string,
	rerunOf *string,
) (uuid.UUID, error) {
	var runNumber int64
	err := transaction.QueryRow(ctx, `
		UPDATE repository_counters
		SET next_ci_run_number = next_ci_run_number + 1
		WHERE repository_id = $1
		RETURNING next_ci_run_number - 1
	`, repository.ID).Scan(&runNumber)
	if err != nil {
		return uuid.Nil, fmt.Errorf("allocate CI run number: %w", err)
	}
	runID := uuid.New()
	var runAttempt int
	if rerunOf != nil {
		if err := transaction.QueryRow(ctx, `SELECT run_attempt + 1 FROM ci_runs WHERE id = $1`, *rerunOf).
			Scan(&runAttempt); err != nil {
			return uuid.Nil, fmt.Errorf("allocate CI rerun attempt: %w", err)
		}
	} else {
		runAttempt = 1
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO ci_runs (
			id, repository_id, workflow_id, workflow_revision_id, run_number, run_attempt, rerun_of, event_name,
			branch, revision, actor_id, status, event_payload
		) VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6, $7, $8, $9, $10,
			NULLIF($11, '')::uuid, 'queued', $12)
	`, runID, repository.ID, workflowID, workflowRevisionID, runNumber, runAttempt, rerunOf,
		eventName, branch, revision, actorID, payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("enqueue CI run: %w", err)
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO ci_jobs (id, run_id, name, status)
		VALUES ($1, $2, $3, 'queued')
	`, uuid.New(), runID, workflowName)
	if err != nil {
		return uuid.Nil, fmt.Errorf("enqueue CI job: %w", err)
	}
	return runID, nil
}

func (store *Store) ClaimJob(ctx context.Context, workerID string, lease time.Duration) (*Job, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin CI job claim: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	var jobID string
	err = transaction.QueryRow(ctx, `
		SELECT j.id
		FROM ci_runs run
		JOIN ci_jobs j ON j.run_id = run.id
		WHERE run.status IN ('queued', 'in_progress')
		  AND run.cancel_requested = false
		  AND (j.status = 'queued' OR (j.status = 'in_progress' AND j.lease_expires_at < now()))
		ORDER BY j.queued_at
		FOR UPDATE OF run SKIP LOCKED
		LIMIT 1
	`).Scan(&jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select CI job: %w", err)
	}
	if err := transaction.QueryRow(ctx,
		`SELECT id FROM ci_jobs WHERE id = $1 FOR UPDATE`, jobID).Scan(&jobID); err != nil {
		return nil, fmt.Errorf("lock CI job: %w", err)
	}
	commandTag, err := transaction.Exec(ctx, `
		UPDATE ci_jobs j
		SET status = 'in_progress', started_at = COALESCE(j.started_at, now()),
		    lease_owner = $2, lease_expires_at = now() + $3::interval,
		    attempt = j.attempt + CASE WHEN j.started_at IS NULL THEN 0 ELSE 1 END
		FROM ci_runs run
		WHERE j.id = $1 AND run.id = j.run_id AND run.cancel_requested = false
		  AND (j.status = 'queued' OR j.lease_expires_at < now())
	`, jobID, workerID, lease.String())
	if err != nil {
		return nil, fmt.Errorf("claim CI job: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return nil, errors.New("CI job was no longer claimable")
	}

	var job Job
	err = transaction.QueryRow(ctx, `
		SELECT j.id, j.attempt, run.id, COALESCE(workflow.path, workflow_revision.path, ''), r.id,
		       o.slug, r.slug, r.lore_url, run.revision, run.branch, run.event_name, run.event_payload
		FROM ci_jobs j
		JOIN ci_runs run ON run.id = j.run_id
		LEFT JOIN ci_workflows workflow ON workflow.id = run.workflow_id
		LEFT JOIN ci_workflow_revisions workflow_revision ON workflow_revision.id = run.workflow_revision_id
		JOIN repositories r ON r.id = run.repository_id
		JOIN organizations o ON o.id = r.organization_id
		WHERE j.id = $1
	`, jobID).Scan(
		&job.ID,
		&job.Attempt,
		&job.RunID,
		&job.WorkflowPath,
		&job.RepositoryID,
		&job.Owner,
		&job.Repository,
		&job.LoreURL,
		&job.Revision,
		&job.Branch,
		&job.EventName,
		&job.EventPayload,
	)
	if err != nil {
		return nil, fmt.Errorf("load claimed CI job: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE ci_runs
		SET status = 'in_progress', started_at = COALESCE(started_at, now())
		WHERE id = $1 AND cancel_requested = false
	`, job.RunID); err != nil {
		return nil, fmt.Errorf("start CI run: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit CI job claim: %w", err)
	}
	return &job, nil
}

func (store *Store) HeartbeatJob(
	ctx context.Context,
	jobID string,
	workerID string,
	lease time.Duration,
) error {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin CI heartbeat: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var runStatus string
	var cancelRequested bool
	if err := transaction.QueryRow(ctx, `
		SELECT run.status, run.cancel_requested
		FROM ci_jobs job JOIN ci_runs run ON run.id = job.run_id
		WHERE job.id = $1
		FOR UPDATE OF run
	`, jobID).Scan(&runStatus, &cancelRequested); errors.Is(err, pgx.ErrNoRows) {
		return errors.New("CI job no longer exists")
	} else if err != nil {
		return fmt.Errorf("lock CI run for heartbeat: %w", err)
	}
	if runStatus != "in_progress" || cancelRequested {
		return errors.New("CI job lease is no longer held")
	}
	commandTag, err := transaction.Exec(ctx, `
		UPDATE ci_jobs
		SET lease_expires_at = now() + $3::interval
		WHERE id = $1 AND lease_owner = $2 AND status = 'in_progress'
	`, jobID, workerID, lease.String())
	if err != nil {
		return fmt.Errorf("heartbeat CI job: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return errors.New("CI job lease is no longer held")
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit CI heartbeat: %w", err)
	}
	return nil
}

func (store *Store) CancellationRequested(ctx context.Context, jobID string) (bool, error) {
	var requested bool
	err := store.pool.QueryRow(ctx, `
		SELECT run.status = 'cancelled' OR run.cancel_requested
		FROM ci_jobs j JOIN ci_runs run ON run.id = j.run_id
		WHERE j.id = $1
	`, jobID).Scan(&requested)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, errors.New("CI job no longer exists")
	}
	if err != nil {
		return false, fmt.Errorf("check CI cancellation: %w", err)
	}
	return requested, nil
}

func (store *Store) SetJobLogObjectKey(ctx context.Context, job Job, workerID string, logObjectKey string) error {
	commandTag, err := store.pool.Exec(ctx, `
		UPDATE ci_jobs
		SET log_object_key = $3
		WHERE id = $1 AND lease_owner = $2 AND status = 'in_progress'
	`, job.ID, workerID, logObjectKey)
	if err != nil {
		return fmt.Errorf("set CI log object key: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return errors.New("CI job lease is no longer held")
	}
	return nil
}

func (store *Store) CompleteJob(
	ctx context.Context,
	job Job,
	workerID string,
	conclusion string,
	logObjectKey string,
	artifacts []Artifact,
) error {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin CI completion: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var runStatus string
	if err := transaction.QueryRow(ctx, `SELECT status FROM ci_runs WHERE id = $1 FOR UPDATE`, job.RunID).
		Scan(&runStatus); err != nil {
		return fmt.Errorf("lock CI run for completion: %w", err)
	}
	if runStatus == "cancelled" {
		return errors.New("CI job was cancelled before completion")
	}
	commandTag, err := transaction.Exec(ctx, `
		UPDATE ci_jobs
		SET status = 'completed', conclusion = $2, completed_at = now(),
		    lease_owner = NULL, lease_expires_at = NULL, log_object_key = NULLIF($3, '')
		WHERE id = $1 AND status = 'in_progress' AND lease_owner = $4
	`, job.ID, conclusion, logObjectKey, workerID)
	if err != nil {
		return fmt.Errorf("complete CI job: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return errors.New("CI job lease was lost before completion")
	}
	for _, artifact := range artifacts {
		_, err = transaction.Exec(ctx, `
			INSERT INTO ci_artifacts (id, job_id, name, object_key, size_bytes)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (job_id, object_key) DO UPDATE
			SET name = EXCLUDED.name, size_bytes = EXCLUDED.size_bytes
		`, uuid.New(), job.ID, artifact.Name, artifact.ObjectKey, artifact.Size)
		if err != nil {
			return fmt.Errorf("record CI artifact: %w", err)
		}
	}
	var activeJobs, failedJobs, timedOutJobs, cancelledJobs int64
	err = transaction.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE status IN ('queued', 'in_progress')),
		       COUNT(*) FILTER (WHERE conclusion = 'failure'),
		       COUNT(*) FILTER (WHERE conclusion = 'timed_out'),
		       COUNT(*) FILTER (WHERE conclusion = 'cancelled')
		FROM ci_jobs WHERE run_id = $1
	`, job.RunID).Scan(&activeJobs, &failedJobs, &timedOutJobs, &cancelledJobs)
	if err != nil {
		return fmt.Errorf("aggregate CI run jobs: %w", err)
	}
	if activeJobs > 0 {
		if _, err := transaction.Exec(ctx, `
			UPDATE ci_runs SET status = 'in_progress' WHERE id = $1
		`, job.RunID); err != nil {
			return fmt.Errorf("refresh CI run status: %w", err)
		}
	} else {
		runConclusion := conclusion
		if cancelledJobs > 0 || conclusion == "cancelled" {
			runConclusion = "cancelled"
		} else if timedOutJobs > 0 || conclusion == "timed_out" {
			runConclusion = "timed_out"
		} else if failedJobs > 0 || conclusion == "failure" {
			runConclusion = "failure"
		}
		_, err = transaction.Exec(ctx, `
			UPDATE ci_runs
			SET status = 'completed', conclusion = $2, completed_at = now()
			WHERE id = $1 AND status <> 'cancelled'
		`, job.RunID, runConclusion)
		if err != nil {
			return fmt.Errorf("complete CI run: %w", err)
		}
		if err := recordCompletionEvents(ctx, transaction, job.RunID, runConclusion); err != nil {
			return err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit CI completion: %w", err)
	}
	return nil
}

func recordCompletionEvents(ctx context.Context, transaction pgx.Tx, runID string, conclusion string) error {
	if _, err := transaction.Exec(ctx, `
		INSERT INTO audit_events (
			id, organization_id, repository_id, actor_id, action, target_type, target_id, details
		)
		SELECT $1, r.organization_id, run.repository_id, run.actor_id, 'actions.run.completed', 'ci_run',
		       run.id::text, jsonb_build_object('conclusion', $2::text)
		FROM ci_runs run JOIN repositories r ON r.id = run.repository_id
		WHERE run.id = $3
	`, uuid.New(), conclusion, runID); err != nil {
		return fmt.Errorf("record CI completion audit event: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, 'actions.run.completed', $2, jsonb_build_object('run_id', $2::text, 'conclusion', $3::text))
		ON CONFLICT (topic, event_key) DO NOTHING
	`, uuid.New(), runID, conclusion); err != nil {
		return fmt.Errorf("record CI completion outbox event: %w", err)
	}
	return nil
}
