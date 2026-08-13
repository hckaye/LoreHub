package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/auth"
)

var ErrRunnerLeaseNotHeld = errors.New("runner does not hold the CI job lease")
var ErrRunnerUploadOffset = errors.New("runner upload offset does not match the stored size")
var ErrRunnerUploadInvalid = errors.New("runner upload is invalid")

// RunnerClaimJob authenticates a runner and claims one compatible self-hosted job atomically.
func (store *Store) RunnerClaimJob(
	ctx context.Context,
	credentialDigest []byte,
	credentialKeyID string,
	usedAt time.Time,
	lease time.Duration,
) (*Job, error) {
	if len(credentialDigest) != 32 || strings.TrimSpace(credentialKeyID) == "" ||
		usedAt.IsZero() || lease <= 0 {
		return nil, auth.ErrInvalidRunnerToken
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin runner CI job claim: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	var runnerID, organizationID string
	var repositoryID *string
	var runnerLabelsJSON []byte
	err = transaction.QueryRow(ctx, `
		SELECT runner.id, runner.organization_id, runner.repository_id, runner.labels
		FROM ci_runners runner
		JOIN organizations organization
		  ON organization.id = runner.organization_id AND organization.active
		WHERE runner.credential_digest = $1
		  AND runner.credential_key_id = $2
		  AND runner.revoked_at IS NULL
		  AND runner.credential_expires_at > $3
		  AND runner.user_id IS NULL
		  AND (
		    runner.repository_id IS NULL
		    OR EXISTS (
		      SELECT 1 FROM repositories scoped_repository
		      WHERE scoped_repository.id = runner.repository_id
		        AND scoped_repository.organization_id = runner.organization_id
		        AND scoped_repository.lifecycle_state = 'active'
		        AND scoped_repository.archived_at IS NULL
		    )
		  )
		FOR UPDATE OF runner
	`, credentialDigest, credentialKeyID, usedAt.UTC()).Scan(
		&runnerID, &organizationID, &repositoryID, &runnerLabelsJSON,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.ErrInvalidRunnerToken
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate runner for CI job claim: %w", err)
	}
	var runnerLabels []string
	if err := json.Unmarshal(runnerLabelsJSON, &runnerLabels); err != nil || len(runnerLabels) == 0 {
		return nil, auth.ErrInvalidRunnerToken
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE ci_runners
		SET last_used_at = CASE
		      WHEN last_used_at IS NULL OR last_used_at < $2::timestamptz - interval '5 minutes'
		      THEN $2::timestamptz
		      ELSE last_used_at
		    END,
		    last_seen_at = GREATEST(COALESCE(last_seen_at, $2::timestamptz), $2::timestamptz)
		WHERE id = $1
	`, runnerID, usedAt.UTC()); err != nil {
		return nil, fmt.Errorf("record runner claim activity: %w", err)
	}

	var jobID string
	err = transaction.QueryRow(ctx, `
		SELECT job.id
		FROM ci_runs run
		JOIN ci_jobs job ON job.run_id = run.id
		JOIN repositories repository ON repository.id = run.repository_id
		LEFT JOIN deployments deployment ON deployment.job_id = job.id
		WHERE run.status IN ('queued', 'in_progress')
		  AND NOT run.cancel_requested
		  AND job.execution_target = 'self_hosted'
		  AND repository.organization_id = $1
		  AND ($2::uuid IS NULL OR repository.id = $2)
		  AND repository.lifecycle_state = 'active'
		  AND repository.archived_at IS NULL
		  AND NOT EXISTS (
		    SELECT 1
		    FROM jsonb_array_elements_text(job.runner_labels) required(label)
		    WHERE NOT ($3::jsonb ? required.label)
		  )
		  AND (
		    deployment.id IS NULL
		    OR (deployment.status IN ('queued', 'waiting') AND deployment.wait_until <= now())
		    OR (
		      deployment.status = 'in_progress'
		      AND job.status = 'in_progress'
		      AND job.lease_expires_at < now()
		    )
		  )
		  AND (job.status = 'queued' OR (job.status = 'in_progress' AND job.lease_expires_at < now()))
		ORDER BY job.queued_at
		FOR UPDATE OF run SKIP LOCKED
		LIMIT 1
	`, organizationID, repositoryID, runnerLabelsJSON).Scan(&jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := transaction.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty runner CI job claim: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select runner CI job: %w", err)
	}
	if err := transaction.QueryRow(ctx, `SELECT id FROM ci_jobs WHERE id = $1 FOR UPDATE`, jobID).
		Scan(&jobID); err != nil {
		return nil, fmt.Errorf("lock runner CI job: %w", err)
	}
	command, err := transaction.Exec(ctx, `
		UPDATE ci_jobs job
		SET status = 'in_progress', started_at = COALESCE(job.started_at, now()),
		    lease_owner = $2::text, lease_expires_at = now() + $3::interval,
		    runner_id = $2::uuid,
		    attempt = job.attempt + CASE WHEN job.started_at IS NULL THEN 0 ELSE 1 END
		FROM ci_runs run
		WHERE job.id = $1 AND run.id = job.run_id AND NOT run.cancel_requested
		  AND job.execution_target = 'self_hosted'
		  AND (job.status = 'queued' OR job.lease_expires_at < now())
	`, jobID, runnerID, lease.String())
	if err != nil {
		return nil, fmt.Errorf("claim runner CI job: %w", err)
	}
	if command.RowsAffected() != 1 {
		return nil, errors.New("runner CI job was no longer claimable")
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE deployments
		SET status = 'in_progress', started_at = COALESCE(started_at, now()), updated_at = now()
		WHERE job_id = $1 AND status IN ('queued', 'waiting') AND wait_until <= now()
	`, jobID); err != nil {
		return nil, fmt.Errorf("start runner deployment: %w", err)
	}
	job, err := loadRunnerJob(ctx, transaction, jobID)
	if err != nil {
		return nil, err
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE ci_runs
		SET status = 'in_progress', started_at = COALESCE(started_at, now())
		WHERE id = $1 AND NOT cancel_requested
	`, job.RunID); err != nil {
		return nil, fmt.Errorf("start runner CI run: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit runner CI job claim: %w", err)
	}
	return &job, nil
}

func (store *Store) RunnerLeaseJob(ctx context.Context, jobID string, runnerID string) (Job, error) {
	job, err := loadRunnerJob(ctx, store.pool, jobID, runnerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrRunnerLeaseNotHeld
	}
	return job, err
}

func (store *Store) RunnerHeartbeatJob(
	ctx context.Context,
	jobID string,
	runnerID string,
	lease time.Duration,
) error {
	if lease <= 0 {
		return ErrRunnerLeaseNotHeld
	}
	command, err := store.pool.Exec(ctx, `
		UPDATE ci_jobs job
		SET lease_expires_at = now() + $3::interval
		FROM ci_runs run
		WHERE job.id = $1 AND job.runner_id = $2::uuid AND job.lease_owner = $2::text
		  AND job.execution_target = 'self_hosted' AND job.status = 'in_progress'
		  AND job.lease_expires_at > now()
		  AND run.id = job.run_id AND run.status = 'in_progress' AND NOT run.cancel_requested
	`, jobID, runnerID, lease.String())
	if err != nil {
		return fmt.Errorf("heartbeat runner CI job: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrRunnerLeaseNotHeld
	}
	return nil
}

func (store *Store) RunnerCancellationRequested(
	ctx context.Context,
	jobID string,
	runnerID string,
) (bool, error) {
	var requested bool
	err := store.pool.QueryRow(ctx, `
		SELECT run.status = 'cancelled' OR run.cancel_requested
		FROM ci_jobs job
		JOIN ci_runs run ON run.id = job.run_id
		WHERE job.id = $1 AND job.runner_id = $2::uuid
		  AND job.execution_target = 'self_hosted'
		  AND (
		    (
		      job.lease_owner = $2::text AND job.status = 'in_progress'
		      AND job.lease_expires_at > now()
		    )
		    OR (job.status = 'cancelled' AND run.cancel_requested)
		  )
	`, jobID, runnerID).Scan(&requested)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrRunnerLeaseNotHeld
	}
	if err != nil {
		return false, fmt.Errorf("check runner CI cancellation: %w", err)
	}
	return requested, nil
}

func (store *Store) AppendRunnerJobLog(
	ctx context.Context,
	jobID string,
	runnerID string,
	offset int64,
	content []byte,
	maximum int64,
) (int64, error) {
	if offset < 0 || maximum <= 0 || int64(len(content)) > maximum-offset {
		return 0, ErrRunnerUploadInvalid
	}
	transaction, job, err := store.lockRunnerLease(ctx, jobID, runnerID)
	if err != nil {
		return 0, err
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	objectKey := filepath.ToSlash(filepath.Join(
		job.RepositoryID, job.RunID, job.ID, fmt.Sprintf("attempt-%d.log", job.Attempt),
	))
	path, err := runnerUploadPath(store.logDirectory, objectKey)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return 0, fmt.Errorf("create runner log directory: %w", err)
	}
	size, err := appendRunnerUpload(path, offset, content, maximum)
	if err != nil {
		return 0, err
	}
	command, err := transaction.Exec(ctx, `
		UPDATE ci_jobs
		SET log_object_key = $3
		WHERE id = $1 AND runner_id = $2::uuid AND lease_owner = $2::text
		  AND status = 'in_progress' AND lease_expires_at > now()
	`, jobID, runnerID, objectKey)
	if err != nil {
		return 0, fmt.Errorf("publish runner CI log: %w", err)
	}
	if command.RowsAffected() != 1 {
		return 0, ErrRunnerLeaseNotHeld
	}
	if err := transaction.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit runner CI log upload: %w", err)
	}
	return size, nil
}

func (store *Store) AppendRunnerArtifact(
	ctx context.Context,
	jobID string,
	runnerID string,
	name string,
	offset int64,
	content []byte,
	complete bool,
	maximumFileBytes int64,
	maximumTotalBytes int64,
	maximumCount int,
) (int64, error) {
	name, ok := normalizedRunnerArtifactName(name)
	if !ok || offset < 0 || maximumFileBytes <= 0 || maximumTotalBytes <= 0 || maximumCount <= 0 ||
		int64(len(content)) > maximumFileBytes-offset {
		return 0, ErrRunnerUploadInvalid
	}
	transaction, job, err := store.lockRunnerLease(ctx, jobID, runnerID)
	if err != nil {
		return 0, err
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	objectKey := filepath.ToSlash(filepath.Join(
		job.RepositoryID, job.RunID, job.ID, fmt.Sprintf("attempt-%d", job.Attempt), filepath.FromSlash(name),
	))
	path, err := runnerUploadPath(store.artifactDirectory, objectKey)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return 0, fmt.Errorf("create runner artifact directory: %w", err)
	}
	size, err := appendRunnerUpload(path, offset, content, maximumFileBytes)
	if err != nil {
		return 0, err
	}
	if complete {
		var count int
		var total int64
		if err := transaction.QueryRow(ctx, `
			SELECT COUNT(*) FILTER (WHERE object_key <> $2),
			       COALESCE(SUM(size_bytes) FILTER (WHERE object_key <> $2), 0)
			FROM ci_artifacts WHERE job_id = $1
		`, job.ID, objectKey).Scan(&count, &total); err != nil {
			return 0, fmt.Errorf("read runner artifact quota: %w", err)
		}
		if count >= maximumCount || size > maximumTotalBytes-total {
			return 0, ErrRunnerUploadInvalid
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO ci_artifacts (id, job_id, name, object_key, size_bytes)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (job_id, object_key) DO UPDATE
			SET name = EXCLUDED.name, size_bytes = EXCLUDED.size_bytes
		`, uuid.New(), job.ID, name, objectKey, size); err != nil {
			return 0, fmt.Errorf("record runner CI artifact: %w", err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit runner CI artifact upload: %w", err)
	}
	return size, nil
}

func (store *Store) lockRunnerLease(
	ctx context.Context,
	jobID string,
	runnerID string,
) (pgx.Tx, Job, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, Job{}, fmt.Errorf("begin runner upload: %w", err)
	}
	err = transaction.QueryRow(ctx, `
		SELECT id FROM ci_jobs
		WHERE id = $1 AND runner_id = $2::uuid AND lease_owner = $2::text
		  AND execution_target = 'self_hosted' AND status = 'in_progress'
		  AND lease_expires_at > now()
		FOR UPDATE
	`, jobID, runnerID).Scan(&jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
		return nil, Job{}, ErrRunnerLeaseNotHeld
	}
	if err != nil {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
		return nil, Job{}, fmt.Errorf("lock runner CI job lease: %w", err)
	}
	job, err := loadRunnerJob(ctx, transaction, jobID, runnerID)
	if err != nil {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
		return nil, Job{}, fmt.Errorf("load runner CI job for upload: %w", err)
	}
	return transaction, job, nil
}

func appendRunnerUpload(path string, offset int64, content []byte, maximum int64) (int64, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 0, fmt.Errorf("open runner upload: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat runner upload: %w", err)
	}
	writtenSize := offset + int64(len(content))
	if info.Size() == writtenSize {
		existing := make([]byte, len(content))
		if _, err := file.ReadAt(existing, offset); err != nil && !errors.Is(err, io.EOF) {
			return info.Size(), fmt.Errorf("verify repeated runner upload: %w", err)
		}
		if !bytes.Equal(existing, content) {
			return info.Size(), ErrRunnerUploadOffset
		}
		return info.Size(), nil
	}
	if info.Size() != offset {
		return info.Size(), ErrRunnerUploadOffset
	}
	if writtenSize > maximum {
		return info.Size(), ErrRunnerUploadInvalid
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return info.Size(), fmt.Errorf("seek runner upload: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		return info.Size(), fmt.Errorf("write runner upload: %w", err)
	}
	if err := file.Sync(); err != nil {
		return info.Size(), fmt.Errorf("sync runner upload: %w", err)
	}
	return writtenSize, nil
}

func runnerUploadPath(root string, objectKey string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil || root == "" {
		return "", ErrRunnerUploadInvalid
	}
	path, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(objectKey)))
	if err != nil || path == root || !strings.HasPrefix(path, root+string(filepath.Separator)) {
		return "", ErrRunnerUploadInvalid
	}
	return path, nil
}

func normalizedRunnerArtifactName(name string) (string, bool) {
	name = filepath.ToSlash(strings.TrimSpace(name))
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	if name == "" || len(name) > 1024 || clean != name || clean == "." || clean == ".." ||
		strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") ||
		strings.ContainsAny(clean, "\x00\r\n\\") {
		return "", false
	}
	return clean, true
}

type runnerJobDatabase interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadRunnerJob(
	ctx context.Context,
	database runnerJobDatabase,
	jobID string,
	runnerID ...string,
) (Job, error) {
	extraSQL := ""
	arguments := []any{jobID}
	if len(runnerID) == 1 {
		extraSQL = `
			AND job.status = 'in_progress'
			AND job.execution_target = 'self_hosted'
			AND job.runner_id = $2::uuid
			AND job.lease_owner = $2::text
			AND job.lease_expires_at > now()
		`
		arguments = append(arguments, runnerID[0])
	}
	var job Job
	var runnerLabelsJSON []byte
	err := database.QueryRow(ctx, `
		SELECT job.id, job.attempt, run.id, COALESCE(run.actor_id::text, ''),
		       COALESCE(workflow.path, workflow_revision.path, ''), repository.id,
		       organization.id, organization.slug, repository.slug,
		       repository.lore_repository_id, repository.lore_url,
		       run.revision, run.branch, run.event_name, run.event_payload,
		       COALESCE(deployment.environment_name, ''), job.runner_labels, job.execution_target,
		       COALESCE(job.log_object_key, '')
		FROM ci_jobs job
		JOIN ci_runs run ON run.id = job.run_id
		LEFT JOIN ci_workflows workflow ON workflow.id = run.workflow_id
		LEFT JOIN ci_workflow_revisions workflow_revision ON workflow_revision.id = run.workflow_revision_id
		JOIN repositories repository ON repository.id = run.repository_id
		JOIN organizations organization ON organization.id = repository.organization_id
		LEFT JOIN deployments deployment ON deployment.job_id = job.id
		WHERE job.id = $1
	`+extraSQL, arguments...).Scan(
		&job.ID, &job.Attempt, &job.RunID, &job.ActorID, &job.WorkflowPath,
		&job.RepositoryID, &job.OrganizationID, &job.Owner, &job.Repository,
		&job.LoreRepositoryID, &job.LoreURL, &job.Revision, &job.Branch,
		&job.EventName, &job.EventPayload, &job.Environment, &runnerLabelsJSON,
		&job.ExecutionTarget, &job.LogObjectKey,
	)
	if err != nil {
		return Job{}, err
	}
	if err := json.Unmarshal(runnerLabelsJSON, &job.RunnerLabels); err != nil {
		return Job{}, fmt.Errorf("decode CI job runner labels: %w", err)
	}
	return job, nil
}
