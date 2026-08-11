package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrActionNotFound  = errors.New("actions resource not found")
	ErrActionForbidden = errors.New("actions operation is not permitted")
	ErrActionConflict  = errors.New("actions operation conflicts with current state")
	ErrActionInvalid   = errors.New("actions request is invalid")
)

type WorkflowRecord struct {
	ID               string          `json:"id"`
	Path             string          `json:"path"`
	Name             string          `json:"name"`
	Enabled          bool            `json:"enabled"`
	State            string          `json:"state"`
	ErrorCode        string          `json:"errorCode,omitempty"`
	ErrorMessage     string          `json:"errorMessage,omitempty"`
	LastSeenRevision string          `json:"lastSeenRevision"`
	TriggerConfig    json.RawMessage `json:"triggerConfig"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

type RunRecord struct {
	ID           string     `json:"id"`
	WorkflowID   string     `json:"workflowId"`
	WorkflowName string     `json:"workflowName"`
	WorkflowPath string     `json:"workflowPath"`
	RunNumber    int64      `json:"runNumber"`
	RunAttempt   int        `json:"runAttempt"`
	RerunOf      *string    `json:"rerunOf,omitempty"`
	EventName    string     `json:"eventName"`
	Branch       string     `json:"branch"`
	Revision     string     `json:"revision"`
	ActorID      *string    `json:"actorId,omitempty"`
	Status       string     `json:"status"`
	Conclusion   *string    `json:"conclusion"`
	QueuedAt     time.Time  `json:"queuedAt"`
	StartedAt    *time.Time `json:"startedAt"`
	CompletedAt  *time.Time `json:"completedAt"`
}

type JobRecord struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	Conclusion   *string    `json:"conclusion"`
	Attempt      int        `json:"attempt"`
	LogAvailable bool       `json:"logAvailable"`
	QueuedAt     time.Time  `json:"queuedAt"`
	StartedAt    *time.Time `json:"startedAt"`
	CompletedAt  *time.Time `json:"completedAt"`
}

type ArtifactRecord struct {
	ID        string    `json:"id"`
	JobID     string    `json:"jobId"`
	Name      string    `json:"name"`
	SizeBytes int64     `json:"sizeBytes"`
	CreatedAt time.Time `json:"createdAt"`
}

type RunDetail struct {
	Run       RunRecord        `json:"run"`
	Workflow  WorkflowRecord   `json:"workflow"`
	Jobs      []JobRecord      `json:"jobs"`
	Artifacts []ArtifactRecord `json:"artifacts"`
}

type RunFilter struct {
	EventName string
	Branch    string
	Status    string
	Page      int64
	PerPage   int64
}

type PageRequest struct {
	Page    int64
	PerPage int64
}

type WorkflowPage struct {
	Workflows []WorkflowRecord
	Total     int64
	HasMore   bool
}

type RunPage struct {
	Runs    []RunRecord
	Total   int64
	HasMore bool
}

type FileDownload struct {
	File interface {
		io.Reader
		io.ReaderAt
		io.Seeker
		io.Closer
	}
	Name        string
	Size        int64
	ContentType string
}

func (store *Store) ListWorkflows(
	ctx context.Context,
	owner string,
	slug string,
	actorID string,
	page PageRequest,
) (WorkflowPage, error) {
	if _, err := store.RepositoryForActions(ctx, owner, slug, actorID); err != nil {
		return WorkflowPage{}, err
	}
	page, offset, err := normalizePage(page)
	if err != nil {
		return WorkflowPage{}, err
	}
	var total int64
	if err := store.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM ci_workflows workflow
		JOIN repositories r ON r.id = workflow.repository_id
		JOIN organizations o ON o.id = r.organization_id
		WHERE o.slug = $1 AND r.slug = $2
	`, owner, slug).Scan(&total); err != nil {
		return WorkflowPage{}, fmt.Errorf("count Actions workflows: %w", err)
	}
	rows, err := store.pool.Query(ctx, `
		SELECT workflow.id, workflow.path, workflow.name, workflow.enabled, workflow.state,
		       COALESCE(workflow.error_code, ''), COALESCE(workflow.error_message, ''),
		       workflow.last_seen_revision, workflow.trigger_config, workflow.updated_at
		FROM ci_workflows workflow
		JOIN repositories r ON r.id = workflow.repository_id
		JOIN organizations o ON o.id = r.organization_id
		WHERE o.slug = $1 AND r.slug = $2
		ORDER BY workflow.path
		LIMIT $3 OFFSET $4
	`, owner, slug, page.PerPage+1, offset)
	if err != nil {
		return WorkflowPage{}, fmt.Errorf("list Actions workflows: %w", err)
	}
	defer rows.Close()
	workflows := make([]WorkflowRecord, 0)
	for rows.Next() {
		workflow, err := scanWorkflow(rows)
		if err != nil {
			return WorkflowPage{}, fmt.Errorf("scan Actions workflow: %w", err)
		}
		workflows = append(workflows, workflow)
	}
	if err := rows.Err(); err != nil {
		return WorkflowPage{}, fmt.Errorf("iterate Actions workflows: %w", err)
	}
	hasMore := len(workflows) > int(page.PerPage)
	if hasMore {
		workflows = workflows[:int(page.PerPage)]
	}
	return WorkflowPage{Workflows: workflows, Total: total, HasMore: hasMore}, nil
}

func (store *Store) ListActionRuns(
	ctx context.Context,
	owner string,
	slug string,
	actorID string,
	filter RunFilter,
) (RunPage, error) {
	if _, err := store.RepositoryForActions(ctx, owner, slug, actorID); err != nil {
		return RunPage{}, err
	}
	page, offset, err := normalizePage(PageRequest{Page: filter.Page, PerPage: filter.PerPage})
	if err != nil {
		return RunPage{}, err
	}
	var total int64
	if err := store.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM ci_runs run
		JOIN repositories r ON r.id = run.repository_id
		JOIN organizations o ON o.id = r.organization_id
		WHERE o.slug = $1 AND r.slug = $2
		  AND (run.workflow_id IS NOT NULL OR run.workflow_revision_id IS NOT NULL)
		  AND ($3 = '' OR run.event_name = $3)
		  AND ($4 = '' OR run.branch = $4)
		  AND ($5 = '' OR run.status = $5)
	`, owner, slug, filter.EventName, filter.Branch, filter.Status).Scan(&total); err != nil {
		return RunPage{}, fmt.Errorf("count Actions runs: %w", err)
	}
	rows, err := store.pool.Query(ctx, actionRunQuery+`
		WHERE o.slug = $1 AND r.slug = $2
		  AND (run.workflow_id IS NOT NULL OR run.workflow_revision_id IS NOT NULL)
		  AND ($3 = '' OR run.event_name = $3)
		  AND ($4 = '' OR run.branch = $4)
		  AND ($5 = '' OR run.status = $5)
		ORDER BY run.run_number DESC
		LIMIT $6 OFFSET $7
	`, owner, slug, filter.EventName, filter.Branch, filter.Status, page.PerPage+1, offset)
	if err != nil {
		return RunPage{}, fmt.Errorf("list Actions runs: %w", err)
	}
	defer rows.Close()
	runs := make([]RunRecord, 0)
	for rows.Next() {
		run, err := scanActionRun(rows)
		if err != nil {
			return RunPage{}, fmt.Errorf("scan Actions run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return RunPage{}, fmt.Errorf("iterate Actions runs: %w", err)
	}
	hasMore := len(runs) > int(page.PerPage)
	if hasMore {
		runs = runs[:int(page.PerPage)]
	}
	return RunPage{Runs: runs, Total: total, HasMore: hasMore}, nil
}

func normalizePage(page PageRequest) (PageRequest, int64, error) {
	if page.Page < 1 {
		page.Page = 1
	}
	if page.PerPage < 1 || page.PerPage > 100 {
		page.PerPage = 30
	}
	if page.Page-1 > math.MaxInt64/page.PerPage {
		return PageRequest{}, 0, ErrActionInvalid
	}
	return page, (page.Page - 1) * page.PerPage, nil
}

func (store *Store) ActionRunDetail(
	ctx context.Context,
	owner string,
	slug string,
	runNumber int64,
	actorID string,
) (RunDetail, error) {
	if _, err := store.RepositoryForActions(ctx, owner, slug, actorID); err != nil {
		return RunDetail{}, err
	}
	row := store.pool.QueryRow(ctx, actionRunQuery+`
		WHERE o.slug = $1 AND r.slug = $2
		  AND (run.workflow_id IS NOT NULL OR run.workflow_revision_id IS NOT NULL)
		  AND run.run_number = $3
	`, owner, slug, runNumber)
	run, err := scanActionRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return RunDetail{}, ErrActionNotFound
	}
	if err != nil {
		return RunDetail{}, fmt.Errorf("get Actions run: %w", err)
	}
	workflow, err := store.workflowByID(ctx, run.WorkflowID)
	if err != nil {
		return RunDetail{}, err
	}
	jobs, err := store.jobsForRun(ctx, run.ID)
	if err != nil {
		return RunDetail{}, err
	}
	artifacts, err := store.artifactsForRun(ctx, run.ID)
	if err != nil {
		return RunDetail{}, err
	}
	return RunDetail{Run: run, Workflow: workflow, Jobs: jobs, Artifacts: artifacts}, nil
}

func (store *Store) DispatchWorkflow(
	ctx context.Context,
	access RepositoryAccess,
	workflowRef string,
	branch string,
	revision string,
	inputs map[string]string,
	actorID string,
) (RunRecord, error) {
	if !access.CanWrite {
		return RunRecord{}, ErrActionForbidden
	}
	if strings.TrimSpace(branch) == "" || revision == "" {
		return RunRecord{}, ErrActionInvalid
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RunRecord{}, fmt.Errorf("begin workflow dispatch: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var workflowID, workflowName, workflowPath, state string
	var enabled bool
	var triggerConfig json.RawMessage
	err = transaction.QueryRow(ctx, `
		SELECT id, name, path, enabled, state, trigger_config
		FROM ci_workflows
		WHERE repository_id = $1 AND (id::text = $2 OR path = $2)
		FOR UPDATE
	`, access.ID, workflowRef).Scan(
		&workflowID, &workflowName, &workflowPath, &enabled, &state, &triggerConfig,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RunRecord{}, ErrActionNotFound
	}
	if err != nil {
		return RunRecord{}, fmt.Errorf("find dispatch workflow: %w", err)
	}
	if !enabled || state != "active" {
		return RunRecord{}, ErrActionConflict
	}
	definition, err := workflowFromTriggerConfig(
		workflowPath, workflowName, enabled, state, triggerConfig,
	)
	if err != nil {
		return RunRecord{}, fmt.Errorf("decode workflow dispatch configuration: %w", err)
	}
	if !definition.WorkflowDispatch {
		return RunRecord{}, ErrActionConflict
	}
	resolvedInputs, err := ResolveWorkflowDispatchInputs(definition, inputs)
	if err != nil {
		return RunRecord{}, fmt.Errorf("%w: %v", ErrActionInvalid, err)
	}
	payload, err := json.Marshal(map[string]any{
		"ref":        "refs/heads/" + branch,
		"after":      revision,
		"repository": map[string]string{"name": access.Slug, "full_name": access.Owner + "/" + access.Slug},
		"workflow":   workflowPath,
		"inputs":     resolvedInputs,
	})
	if err != nil {
		return RunRecord{}, fmt.Errorf("encode workflow dispatch event: %w", err)
	}
	runID, err := store.enqueueRun(ctx, transaction, Repository{
		ID: access.ID, Owner: access.Owner, Slug: access.Slug, LoreURL: access.LoreURL,
	}, workflowID, nil, workflowName, workflowPath, "workflow_dispatch", branch, revision, payload, actorID, nil)
	if err != nil {
		return RunRecord{}, err
	}
	if err := recordActionEvent(ctx, transaction, access, actorID, "actions.workflow_dispatch", runID.String(),
		map[string]any{"workflow_id": workflowID, "branch": branch, "revision": revision}); err != nil {
		return RunRecord{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return RunRecord{}, fmt.Errorf("commit workflow dispatch: %w", err)
	}
	return store.actionRunByID(ctx, runID.String())
}

func (store *Store) CancelActionRun(
	ctx context.Context,
	access RepositoryAccess,
	runNumber int64,
	actorID string,
) (RunRecord, error) {
	if !access.CanWrite {
		return RunRecord{}, ErrActionForbidden
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RunRecord{}, fmt.Errorf("begin run cancellation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var runID, status string
	err = transaction.QueryRow(ctx, `
	SELECT id, status FROM ci_runs
		WHERE repository_id = $1
		  AND (workflow_id IS NOT NULL OR workflow_revision_id IS NOT NULL)
		  AND run_number = $2
		FOR UPDATE
	`, access.ID, runNumber).Scan(&runID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return RunRecord{}, ErrActionNotFound
	}
	if err != nil {
		return RunRecord{}, fmt.Errorf("find run to cancel: %w", err)
	}
	if status == "completed" {
		return RunRecord{}, ErrActionConflict
	}
	if status != "cancelled" {
		if _, err := transaction.Exec(ctx, `
			UPDATE ci_runs
			SET status = 'cancelled', conclusion = 'cancelled', cancel_requested = true,
				completed_at = COALESCE(completed_at, now())
			WHERE id = $1
		`, runID); err != nil {
			return RunRecord{}, fmt.Errorf("cancel run: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			UPDATE ci_jobs
			SET status = 'cancelled', conclusion = 'cancelled', completed_at = COALESCE(completed_at, now()),
				lease_owner = NULL, lease_expires_at = NULL
			WHERE run_id = $1 AND status IN ('queued', 'in_progress')
		`, runID); err != nil {
			return RunRecord{}, fmt.Errorf("cancel run jobs: %w", err)
		}
		if err := recordActionEvent(ctx, transaction, access, actorID, "actions.run.cancelled", runID,
			map[string]any{"run_number": runNumber}); err != nil {
			return RunRecord{}, err
		}
		if err := recordCompletionEvents(ctx, transaction, runID, "cancelled"); err != nil {
			return RunRecord{}, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return RunRecord{}, fmt.Errorf("commit run cancellation: %w", err)
	}
	return store.actionRunByID(ctx, runID)
}

func (store *Store) RerunActionRun(
	ctx context.Context,
	access RepositoryAccess,
	runNumber int64,
	actorID string,
) (RunRecord, error) {
	if !access.CanWrite {
		return RunRecord{}, ErrActionForbidden
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RunRecord{}, fmt.Errorf("begin run rerun: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var originalID, workflowID, workflowRevisionID, workflowName, workflowPath, eventName, branch, revision, status string
	var payload []byte
	err = transaction.QueryRow(ctx, `
		SELECT run.id, COALESCE(run.workflow_id::text, ''), COALESCE(run.workflow_revision_id::text, ''),
		       COALESCE(workflow.name, revision_workflow.name), COALESCE(workflow.path, revision_workflow.path),
		       run.event_name, run.branch,
		       run.revision, run.status, run.event_payload
		FROM ci_runs run
		LEFT JOIN ci_workflows workflow ON workflow.id = run.workflow_id
		LEFT JOIN ci_workflow_revisions revision_workflow ON revision_workflow.id = run.workflow_revision_id
		WHERE run.repository_id = $1
		  AND (run.workflow_id IS NOT NULL OR run.workflow_revision_id IS NOT NULL)
		  AND run.run_number = $2
		FOR UPDATE OF run
	`, access.ID, runNumber).Scan(&originalID, &workflowID, &workflowRevisionID, &workflowName, &workflowPath,
		&eventName, &branch, &revision, &status, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return RunRecord{}, ErrActionNotFound
	}
	if err != nil {
		return RunRecord{}, fmt.Errorf("find run to rerun: %w", err)
	}
	if status != "completed" && status != "cancelled" {
		return RunRecord{}, ErrActionConflict
	}
	rerunID := originalID
	var revisionWorkflowID *uuid.UUID
	if workflowRevisionID != "" {
		parsed, parseErr := uuid.Parse(workflowRevisionID)
		if parseErr != nil {
			return RunRecord{}, fmt.Errorf("parse workflow revision %q: %w", workflowRevisionID, parseErr)
		}
		revisionWorkflowID = &parsed
	}
	newID, err := store.enqueueRun(ctx, transaction, Repository{
		ID: access.ID, Owner: access.Owner, Slug: access.Slug, LoreURL: access.LoreURL,
	}, workflowID, revisionWorkflowID, workflowName, workflowPath, eventName, branch, revision, payload,
		actorID, &rerunID)
	if err != nil {
		return RunRecord{}, err
	}
	if err := recordActionEvent(ctx, transaction, access, actorID, "actions.run.rerun", newID.String(),
		map[string]any{"rerun_of": originalID, "run_number": runNumber}); err != nil {
		return RunRecord{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return RunRecord{}, fmt.Errorf("commit run rerun: %w", err)
	}
	return store.actionRunByID(ctx, newID.String())
}

func (store *Store) OpenActionJobLog(
	ctx context.Context,
	owner string,
	slug string,
	jobID string,
	actorID string,
) (FileDownload, error) {
	access, err := store.RepositoryForActions(ctx, owner, slug, actorID)
	if err != nil {
		return FileDownload{}, err
	}
	var objectKey string
	err = store.pool.QueryRow(ctx, `
		SELECT COALESCE(job.log_object_key, '')
		FROM ci_jobs job JOIN ci_runs run ON run.id = job.run_id
		WHERE job.id = $1 AND run.repository_id = $2
		  AND (run.workflow_id IS NOT NULL OR run.workflow_revision_id IS NOT NULL)
	`, jobID, access.ID).Scan(&objectKey)
	if errors.Is(err, pgx.ErrNoRows) || objectKey == "" {
		return FileDownload{}, ErrActionNotFound
	}
	if err != nil {
		return FileDownload{}, fmt.Errorf("find CI job log: %w", err)
	}
	return store.openFile(store.logDirectory, objectKey, "job-"+jobID+".log", "text/plain; charset=utf-8")
}

func (store *Store) OpenActionArtifact(
	ctx context.Context,
	owner string,
	slug string,
	artifactID string,
	actorID string,
) (FileDownload, error) {
	access, err := store.RepositoryForActions(ctx, owner, slug, actorID)
	if err != nil {
		return FileDownload{}, err
	}
	var objectKey, name string
	var size int64
	err = store.pool.QueryRow(ctx, `
		SELECT artifact.object_key, artifact.name, artifact.size_bytes
		FROM ci_artifacts artifact
		JOIN ci_jobs job ON job.id = artifact.job_id
		JOIN ci_runs run ON run.id = job.run_id
		WHERE artifact.id = $1 AND run.repository_id = $2
		  AND (run.workflow_id IS NOT NULL OR run.workflow_revision_id IS NOT NULL)
	`, artifactID, access.ID).Scan(&objectKey, &name, &size)
	if errors.Is(err, pgx.ErrNoRows) {
		return FileDownload{}, ErrActionNotFound
	}
	if err != nil {
		return FileDownload{}, fmt.Errorf("find CI artifact: %w", err)
	}
	download, err := store.openFile(store.artifactDirectory, objectKey, filepath.Base(name), "application/octet-stream")
	if err != nil {
		return FileDownload{}, err
	}
	if download.Size != size {
		_ = download.File.Close()
		return FileDownload{}, errors.New("persisted CI artifact size does not match its record")
	}
	return download, nil
}

func (store *Store) workflowByID(ctx context.Context, workflowID string) (WorkflowRecord, error) {
	if workflowID == "" {
		return WorkflowRecord{}, nil
	}
	row := store.pool.QueryRow(ctx, `
		SELECT id, path, name, enabled, state, COALESCE(error_code, ''), COALESCE(error_message, ''),
		       last_seen_revision, trigger_config, updated_at
		FROM ci_workflows WHERE id = $1
	`, workflowID)
	workflow, err := scanWorkflow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		row = store.pool.QueryRow(ctx, `
			SELECT id, path, name, enabled, state, COALESCE(error_code, ''), COALESCE(error_message, ''),
			       revision, trigger_config, created_at
			FROM ci_workflow_revisions WHERE id = $1
		`, workflowID)
		workflow, err = scanWorkflow(row)
		if errors.Is(err, pgx.ErrNoRows) {
			return WorkflowRecord{}, ErrActionNotFound
		}
	}
	if err != nil {
		return WorkflowRecord{}, fmt.Errorf("get CI workflow: %w", err)
	}
	return workflow, nil
}

func (store *Store) actionRunByID(ctx context.Context, runID string) (RunRecord, error) {
	return scanActionRun(store.pool.QueryRow(ctx, actionRunQuery+` WHERE run.id = $1`, runID))
}

func (store *Store) actionRunsByIDs(ctx context.Context, runIDs []uuid.UUID) ([]RunRecord, error) {
	runs := make([]RunRecord, 0, len(runIDs))
	for _, runID := range runIDs {
		run, err := store.actionRunByID(ctx, runID.String())
		if err != nil {
			return nil, fmt.Errorf("load Actions run %s: %w", runID, err)
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func (store *Store) jobsForRun(ctx context.Context, runID string) ([]JobRecord, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id, name, status, conclusion, attempt, log_object_key IS NOT NULL,
		       queued_at, started_at, completed_at
		FROM ci_jobs WHERE run_id = $1 ORDER BY name, attempt
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("list CI jobs: %w", err)
	}
	defer rows.Close()
	jobs := make([]JobRecord, 0)
	for rows.Next() {
		var job JobRecord
		if err := rows.Scan(&job.ID, &job.Name, &job.Status, &job.Conclusion, &job.Attempt, &job.LogAvailable,
			&job.QueuedAt, &job.StartedAt, &job.CompletedAt); err != nil {
			return nil, fmt.Errorf("scan CI job: %w", err)
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (store *Store) artifactsForRun(ctx context.Context, runID string) ([]ArtifactRecord, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT artifact.id, artifact.job_id, artifact.name, artifact.size_bytes, artifact.created_at
		FROM ci_artifacts artifact JOIN ci_jobs job ON job.id = artifact.job_id
		WHERE job.run_id = $1 ORDER BY artifact.created_at, artifact.name
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("list CI artifacts: %w", err)
	}
	defer rows.Close()
	artifacts := make([]ArtifactRecord, 0)
	for rows.Next() {
		var artifact ArtifactRecord
		if err := rows.Scan(
			&artifact.ID, &artifact.JobID, &artifact.Name, &artifact.SizeBytes, &artifact.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan CI artifact: %w", err)
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func (store *Store) openFile(root string, objectKey string, name string, contentType string) (FileDownload, error) {
	path, err := safeStoragePath(root, objectKey)
	if err != nil {
		return FileDownload{}, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return FileDownload{}, ErrActionNotFound
	}
	if err != nil {
		return FileDownload{}, fmt.Errorf("open Actions file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return FileDownload{}, fmt.Errorf("stat Actions file: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return FileDownload{}, errors.New("Actions file is not regular")
	}
	return FileDownload{File: file, Name: name, Size: info.Size(), ContentType: contentType}, nil
}

func safeStoragePath(root string, objectKey string) (string, error) {
	if root == "" || objectKey == "" || strings.ContainsAny(objectKey, "\\\x00") {
		return "", ErrActionNotFound
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve Actions storage root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrActionNotFound
		}
		return "", fmt.Errorf("resolve Actions storage root symlinks: %w", err)
	}
	path, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(objectKey)))
	if err != nil {
		return "", fmt.Errorf("resolve Actions storage path: %w", err)
	}
	if path != root && !strings.HasPrefix(path, root+string(filepath.Separator)) {
		return "", errors.New("Actions storage path escapes its root")
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", fmt.Errorf("resolve Actions storage path relative name: %w", err)
	}
	current := root
	parts := strings.Split(relative, string(filepath.Separator))
	for index, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return "", ErrActionNotFound
			}
			return "", fmt.Errorf("inspect Actions storage path: %w", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("Actions storage path contains a symlink")
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", errors.New("Actions storage path contains a non-directory")
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrActionNotFound
		}
		return "", fmt.Errorf("inspect Actions storage path: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("Actions storage path is not a regular file")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrActionNotFound
		}
		return "", fmt.Errorf("resolve Actions storage path: %w", err)
	}
	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return "", errors.New("Actions storage symlink escapes its root")
	}
	return resolved, nil
}

func recordActionEvent(
	ctx context.Context,
	transaction pgx.Tx,
	access RepositoryAccess,
	actorID string,
	action string,
	targetID string,
	details map[string]any,
) error {
	payload := make(map[string]any, len(details)+2)
	for key, value := range details {
		payload[key] = value
	}
	payload["organizationId"] = access.OrganizationID
	payload["repositoryId"] = access.ID
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode Actions audit details: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		INSERT INTO audit_events (
			id, organization_id, repository_id, actor_id, action, target_type, target_id, details
		) VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, 'ci_run', $6, $7)
	`, uuid.New(), access.OrganizationID, access.ID, actorID, action, targetID, encoded); err != nil {
		return fmt.Errorf("record Actions audit event: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (topic, event_key) DO NOTHING
	`, uuid.New(), action, targetID, encoded); err != nil {
		return fmt.Errorf("record Actions outbox event: %w", err)
	}
	return nil
}

const actionRunQuery = `
	SELECT run.id, COALESCE(run.workflow_id::text, run.workflow_revision_id::text, ''),
	       COALESCE(workflow.name, revision_workflow.name, ''),
	       COALESCE(workflow.path, revision_workflow.path, ''),
	       run.run_number, run.run_attempt, run.rerun_of, run.event_name, run.branch, run.revision,
	       run.actor_id, run.status, run.conclusion, run.queued_at, run.started_at, run.completed_at
	FROM ci_runs run
	LEFT JOIN ci_workflows workflow ON workflow.id = run.workflow_id
	LEFT JOIN ci_workflow_revisions revision_workflow ON revision_workflow.id = run.workflow_revision_id
	JOIN repositories r ON r.id = run.repository_id
	JOIN organizations o ON o.id = r.organization_id
`

type rowScanner interface {
	Scan(destination ...any) error
}

func scanWorkflow(row rowScanner) (WorkflowRecord, error) {
	var workflow WorkflowRecord
	err := row.Scan(&workflow.ID, &workflow.Path, &workflow.Name, &workflow.Enabled, &workflow.State,
		&workflow.ErrorCode, &workflow.ErrorMessage, &workflow.LastSeenRevision, &workflow.TriggerConfig,
		&workflow.UpdatedAt)
	return workflow, err
}

func scanActionRun(row rowScanner) (RunRecord, error) {
	var run RunRecord
	err := row.Scan(&run.ID, &run.WorkflowID, &run.WorkflowName, &run.WorkflowPath, &run.RunNumber,
		&run.RunAttempt, &run.RerunOf, &run.EventName, &run.Branch, &run.Revision, &run.ActorID, &run.Status,
		&run.Conclusion, &run.QueuedAt, &run.StartedAt, &run.CompletedAt)
	return run, err
}
