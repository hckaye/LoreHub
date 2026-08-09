package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	ID               string
	Owner            string
	Slug             string
	LoreRepositoryID string
	LoreURL          string
}

type ObservedBranch struct {
	ID             string
	Name           string
	LatestRevision string
}

type Job struct {
	ID               string
	Attempt          int
	RunID            string
	RepositoryID     string
	Owner            string
	Repository       string
	LoreRepositoryID string
	LoreURL          string
	Revision         string
	Branch           string
	EventName        string
	EventPayload     json.RawMessage
}

type Artifact struct {
	Name      string
	ObjectKey string
	Size      int64
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (store *Store) Repositories(ctx context.Context) ([]Repository, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT r.id, o.slug, r.slug, r.lore_repository_id, r.lore_url
		FROM repositories r
		JOIN organizations o ON o.id = r.organization_id AND o.active
		WHERE r.archived_at IS NULL AND r.lifecycle_state = 'active'
		ORDER BY r.id
	`)
	if err != nil {
		return nil, fmt.Errorf("list repositories for branch polling: %w", err)
	}
	defer rows.Close()
	repositories := make([]Repository, 0)
	for rows.Next() {
		var repository Repository
		if err := rows.Scan(&repository.ID, &repository.Owner, &repository.Slug,
			&repository.LoreRepositoryID, &repository.LoreURL); err != nil {
			return nil, fmt.Errorf("scan repository for branch polling: %w", err)
		}
		repositories = append(repositories, repository)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repositories for branch polling: %w", err)
	}
	return repositories, nil
}

func (store *Store) ReconcileBranchStates(
	ctx context.Context,
	repositoryID string,
	observedBranchIDs []string,
) error {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin branch reconciliation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := transaction.Exec(ctx, `
		DELETE FROM repository_branch_states
		WHERE repository_id = $1 AND branch_id <> ALL($2::text[])
	`, repositoryID, observedBranchIDs); err != nil {
		return fmt.Errorf("remove missed Lore branch observations: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit branch reconciliation: %w", err)
	}
	return nil
}

func (store *Store) ObserveBranch(
	ctx context.Context,
	repository Repository,
	branch ObservedBranch,
) (bool, error) {
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
	if err := store.enqueuePush(ctx, transaction, repository, branch, previous); err != nil {
		return false, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit branch observation: %w", err)
	}
	return true, nil
}

func (store *Store) enqueuePush(
	ctx context.Context,
	transaction pgx.Tx,
	repository Repository,
	branch ObservedBranch,
	previousRevision string,
) error {
	var runNumber int64
	err := transaction.QueryRow(ctx, `
		UPDATE repository_counters
		SET next_ci_run_number = next_ci_run_number + 1
		WHERE repository_id = $1
		RETURNING next_ci_run_number - 1
	`, repository.ID).Scan(&runNumber)
	if err != nil {
		return fmt.Errorf("allocate CI run number: %w", err)
	}
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
	runID := uuid.New()
	_, err = transaction.Exec(ctx, `
		INSERT INTO ci_runs (
			id, repository_id, run_number, event_name, branch, revision, status, event_payload
		) VALUES ($1, $2, $3, 'push', $4, $5, 'queued', $6)
	`, runID, repository.ID, runNumber, branch.Name, branch.LatestRevision, payload)
	if err != nil {
		return fmt.Errorf("enqueue CI run: %w", err)
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO ci_jobs (id, run_id, name, status)
		VALUES ($1, $2, 'workflow', 'queued')
	`, uuid.New(), runID)
	if err != nil {
		return fmt.Errorf("enqueue CI job: %w", err)
	}
	return nil
}

func (store *Store) ClaimJob(ctx context.Context, workerID string, lease time.Duration) (*Job, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin CI job claim: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	var jobID string
	err = transaction.QueryRow(ctx, `
		SELECT id
		FROM ci_jobs
		WHERE status = 'queued'
		   OR (status = 'in_progress' AND lease_expires_at < now())
		ORDER BY queued_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`).Scan(&jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select CI job: %w", err)
	}
	_, err = transaction.Exec(ctx, `
		UPDATE ci_jobs
		SET status = 'in_progress', started_at = COALESCE(started_at, now()),
		    lease_owner = $2, lease_expires_at = now() + $3::interval,
		    attempt = attempt + CASE WHEN started_at IS NULL THEN 0 ELSE 1 END
		WHERE id = $1
	`, jobID, workerID, lease.String())
	if err != nil {
		return nil, fmt.Errorf("claim CI job: %w", err)
	}

	var job Job
	err = transaction.QueryRow(ctx, `
		SELECT j.id, j.attempt, run.id, r.id, o.slug, r.slug, r.lore_repository_id, r.lore_url,
		       run.revision, run.branch, run.event_name, run.event_payload
		FROM ci_jobs j
		JOIN ci_runs run ON run.id = j.run_id
		JOIN repositories r ON r.id = run.repository_id
		JOIN organizations o ON o.id = r.organization_id
		WHERE j.id = $1
	`, jobID).Scan(
		&job.ID,
		&job.Attempt,
		&job.RunID,
		&job.RepositoryID,
		&job.Owner,
		&job.Repository,
		&job.LoreRepositoryID,
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
		UPDATE ci_runs SET status = 'in_progress', started_at = COALESCE(started_at, now()) WHERE id = $1
	`, job.RunID); err != nil {
		return nil, fmt.Errorf("start CI run: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit CI job claim: %w", err)
	}
	return &job, nil
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
	commandTag, err := transaction.Exec(ctx, `
		UPDATE ci_jobs
		SET status = 'completed', conclusion = $2, completed_at = now(),
		    lease_owner = NULL, lease_expires_at = NULL, log_object_key = $3
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
	_, err = transaction.Exec(ctx, `
		UPDATE ci_runs
		SET status = 'completed', conclusion = $2, completed_at = now()
		WHERE id = $1
	`, job.RunID, conclusion)
	if err != nil {
		return fmt.Errorf("complete CI run: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit CI completion: %w", err)
	}
	return nil
}
