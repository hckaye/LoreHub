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
)

const (
	defaultSARIFListLimit = 50
	maxSARIFListLimit     = 100
	maxSARIFAlertLimit    = 1000
)

type SARIFJobClaims struct {
	RepositoryID string
	RunID        string
	JobID        string
	Attempt      int
}

type SARIFUploadInput struct {
	Claims           SARIFJobClaims
	Owner            string
	Repository       string
	ExpectedRevision string
	ExpectedRef      string
	Document         []byte
}

type SARIFRepositorySelector struct {
	RepositoryID string
	Owner        string
	Repository   string
}

type SARIFUploadMetadata struct {
	ID           string    `json:"id"`
	RepositoryID string    `json:"repositoryId"`
	RunID        string    `json:"runId"`
	JobID        string    `json:"jobId"`
	Attempt      int       `json:"attempt"`
	Tools        []string  `json:"tools"`
	Revision     string    `json:"revision"`
	Ref          string    `json:"ref"`
	Version      string    `json:"version"`
	DocumentSize int       `json:"documentSize"`
	ResultsCount int       `json:"resultsCount"`
	CreatedAt    time.Time `json:"createdAt"`
}

type CodeScanningAlert struct {
	ID           string    `json:"id"`
	UploadID     string    `json:"uploadId"`
	RepositoryID string    `json:"repositoryId"`
	ToolName     string    `json:"tool"`
	RuleID       string    `json:"ruleId"`
	Level        string    `json:"level"`
	Message      string    `json:"message"`
	Path         string    `json:"path"`
	StartLine    *int      `json:"startLine,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

func (store *Store) UploadSARIF(ctx context.Context, input SARIFUploadInput) (SARIFUploadMetadata, error) {
	claims, err := validateSARIFClaims(input.Claims)
	if err != nil {
		return SARIFUploadMetadata{}, err
	}
	if err := validateSARIFExpectedRevisionAndRef(input.ExpectedRevision, input.ExpectedRef); err != nil {
		return SARIFUploadMetadata{}, err
	}
	selector, err := validateSARIFRepositorySelector(SARIFRepositorySelector{
		RepositoryID: claims.RepositoryID, Owner: input.Owner, Repository: input.Repository,
	})
	if err != nil {
		return SARIFUploadMetadata{}, err
	}
	parsed, err := parseSARIF(input.Document)
	if err != nil {
		return SARIFUploadMetadata{}, err
	}
	toolsJSON, err := json.Marshal(parsed.Tools)
	if err != nil {
		return SARIFUploadMetadata{}, fmt.Errorf("encode SARIF tools: %w", err)
	}

	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return SARIFUploadMetadata{}, fmt.Errorf("begin SARIF upload: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	var organizationID string
	err = transaction.QueryRow(ctx, `
		SELECT organization.id::text
		FROM repositories repository
		JOIN organizations organization ON organization.id = repository.organization_id
		WHERE repository.id = $1 AND organization.slug = $2 AND repository.slug = $3
		  AND repository.archived_at IS NULL AND repository.migrating_at IS NULL
		  AND repository.lifecycle_state = 'active'
		  AND organization.active
		FOR SHARE OF repository, organization
	`, selector.RepositoryID, selector.Owner, selector.Repository).Scan(&organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SARIFUploadMetadata{}, ErrSARIFNotFound
	}
	if err != nil {
		return SARIFUploadMetadata{}, fmt.Errorf("verify SARIF repository boundary: %w", err)
	}

	var revision, ref, actorID string
	err = transaction.QueryRow(ctx, `
		SELECT run.revision, run.branch, COALESCE(run.actor_id::text, '')
		FROM ci_jobs job
		JOIN ci_runs run ON run.id = job.run_id
		WHERE job.id = $1 AND job.run_id = $2 AND job.attempt = $3
		  AND run.id = $2 AND run.repository_id = $4
		  AND job.status = 'in_progress' AND run.status = 'in_progress'
		  AND NOT run.cancel_requested
		  AND NULLIF(BTRIM(job.lease_owner), '') IS NOT NULL
		  AND job.lease_expires_at > now()
		FOR SHARE OF job, run
	`, claims.JobID, claims.RunID, claims.Attempt, claims.RepositoryID).
		Scan(&revision, &ref, &actorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SARIFUploadMetadata{}, ErrSARIFBoundary
	}
	if err != nil {
		return SARIFUploadMetadata{}, fmt.Errorf("verify SARIF job boundary: %w", err)
	}
	canonicalRef := ref
	if !strings.HasPrefix(canonicalRef, "refs/") {
		canonicalRef = "refs/heads/" + canonicalRef
	}
	if revision != input.ExpectedRevision || canonicalRef != input.ExpectedRef {
		return SARIFUploadMetadata{}, ErrSARIFBoundary
	}
	ref = canonicalRef

	uploadID := uuid.New()
	metadata := SARIFUploadMetadata{
		ID: uploadID.String(), RepositoryID: claims.RepositoryID, RunID: claims.RunID,
		JobID: claims.JobID, Attempt: claims.Attempt, Tools: parsed.Tools, Revision: revision, Ref: ref,
		Version: parsed.Version, DocumentSize: len(input.Document), ResultsCount: len(parsed.Alerts),
	}
	err = transaction.QueryRow(ctx, `
		INSERT INTO ci_sarif_uploads (
			id, repository_id, run_id, job_id, attempt, tools, revision, ref,
			sarif_version, document_size, results_count, sarif
		) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10, $11, $12::jsonb)
		RETURNING created_at
	`, uploadID, claims.RepositoryID, claims.RunID, claims.JobID, claims.Attempt, string(toolsJSON),
		revision, ref, parsed.Version, len(input.Document), len(parsed.Alerts), string(input.Document)).
		Scan(&metadata.CreatedAt)
	if err != nil {
		return SARIFUploadMetadata{}, fmt.Errorf("store SARIF upload: %w", err)
	}
	if err := copySARIFAlerts(ctx, transaction, uploadID, claims.RepositoryID, parsed.Alerts); err != nil {
		return SARIFUploadMetadata{}, err
	}
	if err := recordSARIFAudit(ctx, transaction, metadata, organizationID, actorID); err != nil {
		return SARIFUploadMetadata{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return SARIFUploadMetadata{}, fmt.Errorf("commit SARIF upload: %w", err)
	}
	return metadata, nil
}

func (store *Store) ListSARIFUploads(
	ctx context.Context,
	selector SARIFRepositorySelector,
	limit int,
) ([]SARIFUploadMetadata, error) {
	selector, err := validateSARIFRepositorySelector(selector)
	if err != nil {
		return nil, err
	}
	limit, err = normalizeSARIFListLimit(limit, maxSARIFListLimit)
	if err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, sarifMetadataQuery+`
		WHERE upload.repository_id = $1 AND organization.slug = $2 AND repository.slug = $3
		  AND repository.lifecycle_state = 'active'
		  AND organization.active
		ORDER BY upload.created_at DESC, upload.id DESC
		LIMIT $4
	`, selector.RepositoryID, selector.Owner, selector.Repository, limit)
	if err != nil {
		return nil, fmt.Errorf("list SARIF uploads: %w", err)
	}
	defer rows.Close()
	uploads := make([]SARIFUploadMetadata, 0)
	for rows.Next() {
		upload, scanErr := scanSARIFUploadMetadata(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		uploads = append(uploads, upload)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list SARIF uploads: %w", err)
	}
	return uploads, nil
}

func (store *Store) GetSARIFUpload(
	ctx context.Context,
	selector SARIFRepositorySelector,
	uploadID string,
) (SARIFUploadMetadata, error) {
	selector, err := validateSARIFRepositorySelector(selector)
	if err != nil {
		return SARIFUploadMetadata{}, err
	}
	uploadUUID, err := exactSARIFUUID("upload", uploadID)
	if err != nil {
		return SARIFUploadMetadata{}, err
	}
	upload, err := scanSARIFUploadMetadata(store.pool.QueryRow(ctx, sarifMetadataQuery+`
		WHERE upload.id = $1 AND upload.repository_id = $2
		  AND organization.slug = $3 AND repository.slug = $4
		  AND repository.lifecycle_state = 'active'
		  AND organization.active
	`, uploadUUID, selector.RepositoryID, selector.Owner, selector.Repository))
	if errors.Is(err, pgx.ErrNoRows) {
		return SARIFUploadMetadata{}, ErrSARIFNotFound
	}
	if err != nil {
		return SARIFUploadMetadata{}, err
	}
	return upload, nil
}

func (store *Store) ListCodeScanningAlerts(
	ctx context.Context,
	selector SARIFRepositorySelector,
	uploadID string,
	limit int,
) ([]CodeScanningAlert, error) {
	selector, err := validateSARIFRepositorySelector(selector)
	if err != nil {
		return nil, err
	}
	limit, err = normalizeSARIFListLimit(limit, maxSARIFAlertLimit)
	if err != nil {
		return nil, err
	}
	var uploadUUID *uuid.UUID
	if uploadID != "" {
		parsed, parseErr := exactSARIFUUID("upload", uploadID)
		if parseErr != nil {
			return nil, parseErr
		}
		uploadUUID = &parsed
	}
	rows, err := store.pool.Query(ctx, `
		SELECT alert.id::text, alert.upload_id::text, alert.repository_id::text,
		       alert.tool_name, alert.rule_id, alert.level, alert.message, alert.path,
		       alert.start_line, alert.created_at
		FROM ci_code_scanning_alerts alert
		JOIN ci_sarif_uploads upload
		  ON upload.id = alert.upload_id AND upload.repository_id = alert.repository_id
		JOIN repositories repository ON repository.id = alert.repository_id
		JOIN organizations organization ON organization.id = repository.organization_id
		WHERE alert.repository_id = $1 AND organization.slug = $2 AND repository.slug = $3
		  AND ($4::uuid IS NULL OR alert.upload_id = $4)
		  AND repository.lifecycle_state = 'active'
		  AND organization.active
		ORDER BY alert.created_at DESC, alert.id DESC
		LIMIT $5
	`, selector.RepositoryID, selector.Owner, selector.Repository, uploadUUID, limit)
	if err != nil {
		return nil, fmt.Errorf("list code scanning alerts: %w", err)
	}
	defer rows.Close()
	alerts := make([]CodeScanningAlert, 0)
	for rows.Next() {
		var alert CodeScanningAlert
		if err := rows.Scan(
			&alert.ID, &alert.UploadID, &alert.RepositoryID, &alert.ToolName, &alert.RuleID,
			&alert.Level, &alert.Message, &alert.Path, &alert.StartLine, &alert.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan code scanning alert: %w", err)
		}
		alerts = append(alerts, alert)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list code scanning alerts: %w", err)
	}
	return alerts, nil
}

func copySARIFAlerts(
	ctx context.Context,
	transaction pgx.Tx,
	uploadID uuid.UUID,
	repositoryID string,
	alerts []parsedSARIFAlert,
) error {
	if len(alerts) == 0 {
		return nil
	}
	_, err := transaction.CopyFrom(
		ctx,
		pgx.Identifier{"ci_code_scanning_alerts"},
		[]string{"id", "upload_id", "repository_id", "tool_name", "rule_id", "level", "message", "path",
			"start_line"},
		pgx.CopyFromSlice(len(alerts), func(index int) ([]any, error) {
			alert := alerts[index]
			return []any{
				uuid.New(), uploadID, repositoryID, alert.ToolName, alert.RuleID, alert.Level,
				alert.Message, alert.Path, alert.StartLine,
			}, nil
		}),
	)
	if err != nil {
		return fmt.Errorf("store code scanning alerts: %w", err)
	}
	return nil
}

func recordSARIFAudit(
	ctx context.Context,
	transaction pgx.Tx,
	metadata SARIFUploadMetadata,
	organizationID string,
	actorID string,
) error {
	details, err := json.Marshal(map[string]any{
		"runId": metadata.RunID, "jobId": metadata.JobID, "attempt": metadata.Attempt,
		"revision": metadata.Revision, "ref": metadata.Ref, "tools": metadata.Tools,
		"resultsCount": metadata.ResultsCount,
	})
	if err != nil {
		return fmt.Errorf("encode SARIF audit details: %w", err)
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO audit_events (
			id, organization_id, repository_id, actor_id, action, target_type, target_id, details
		) VALUES (
			$1, $2, $3, NULLIF($4, '')::uuid, 'actions.code_scanning.sarif_uploaded',
			'code_scanning_upload', $5, $6::jsonb
		)
	`, uuid.New(), organizationID, metadata.RepositoryID, actorID, metadata.ID, string(details))
	if err != nil {
		return fmt.Errorf("record SARIF audit event: %w", err)
	}
	return nil
}

func validateSARIFClaims(claims SARIFJobClaims) (SARIFJobClaims, error) {
	repositoryID, err := exactSARIFUUID("repository", claims.RepositoryID)
	if err != nil {
		return SARIFJobClaims{}, err
	}
	runID, err := exactSARIFUUID("run", claims.RunID)
	if err != nil {
		return SARIFJobClaims{}, err
	}
	jobID, err := exactSARIFUUID("job", claims.JobID)
	if err != nil {
		return SARIFJobClaims{}, err
	}
	if claims.Attempt <= 0 {
		return SARIFJobClaims{}, fmt.Errorf("%w: attempt must be positive", ErrSARIFBoundary)
	}
	return SARIFJobClaims{
		RepositoryID: repositoryID.String(), RunID: runID.String(), JobID: jobID.String(), Attempt: claims.Attempt,
	}, nil
}

func validateSARIFExpectedRevisionAndRef(revision string, ref string) error {
	if revision == "" || len(revision) > 128 || strings.TrimSpace(revision) != revision ||
		strings.ContainsAny(revision, "\x00\r\n\t ") {
		return fmt.Errorf("%w: expected Lore revision is invalid", ErrSARIFBoundary)
	}
	if !strings.HasPrefix(ref, "refs/heads/") || len(ref) > 255 ||
		strings.TrimSpace(ref) != ref || strings.ContainsAny(ref, "\x00\r\n\t \\") ||
		strings.Contains(ref, "..") || strings.Contains(ref, "//") || strings.Contains(ref, "@{") ||
		strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".") {
		return fmt.Errorf("%w: expected ref is invalid", ErrSARIFBoundary)
	}
	branch := strings.TrimPrefix(ref, "refs/heads/")
	if branch == "" {
		return fmt.Errorf("%w: expected ref is invalid", ErrSARIFBoundary)
	}
	for _, segment := range strings.Split(branch, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("%w: expected ref is invalid", ErrSARIFBoundary)
		}
	}
	return nil
}

func validateSARIFRepositorySelector(
	selector SARIFRepositorySelector,
) (SARIFRepositorySelector, error) {
	repositoryID, err := exactSARIFUUID("repository", selector.RepositoryID)
	if err != nil {
		return SARIFRepositorySelector{}, err
	}
	if selector.Owner == "" || selector.Repository == "" || len(selector.Owner) > 255 ||
		len(selector.Repository) > 255 || strings.TrimSpace(selector.Owner) != selector.Owner ||
		strings.TrimSpace(selector.Repository) != selector.Repository ||
		strings.ContainsAny(selector.Owner+selector.Repository, "\x00\r\n\t/") {
		return SARIFRepositorySelector{}, fmt.Errorf("%w: repository selector is invalid", ErrSARIFBoundary)
	}
	selector.RepositoryID = repositoryID.String()
	return selector, nil
}

func exactSARIFUUID(label string, value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value {
		return uuid.Nil, fmt.Errorf("%w: %s ID is invalid", ErrSARIFBoundary, label)
	}
	return parsed, nil
}

func normalizeSARIFListLimit(limit int, maximum int) (int, error) {
	if limit == 0 {
		return defaultSARIFListLimit, nil
	}
	if limit < 0 || limit > maximum {
		return 0, ErrSARIFListLimit
	}
	return limit, nil
}

const sarifMetadataQuery = `
	SELECT upload.id::text, upload.repository_id::text, upload.run_id::text, upload.job_id::text,
	       upload.attempt, upload.tools, upload.revision, upload.ref, upload.sarif_version,
	       upload.document_size, upload.results_count, upload.created_at
	FROM ci_sarif_uploads upload
	JOIN repositories repository ON repository.id = upload.repository_id
	JOIN organizations organization ON organization.id = repository.organization_id
`

func scanSARIFUploadMetadata(row rowScanner) (SARIFUploadMetadata, error) {
	var metadata SARIFUploadMetadata
	var toolsJSON []byte
	err := row.Scan(
		&metadata.ID, &metadata.RepositoryID, &metadata.RunID, &metadata.JobID, &metadata.Attempt,
		&toolsJSON, &metadata.Revision, &metadata.Ref, &metadata.Version, &metadata.DocumentSize,
		&metadata.ResultsCount, &metadata.CreatedAt,
	)
	if err != nil {
		return SARIFUploadMetadata{}, err
	}
	if err := json.Unmarshal(toolsJSON, &metadata.Tools); err != nil {
		return SARIFUploadMetadata{}, fmt.Errorf("decode SARIF tools: %w", err)
	}
	return metadata, nil
}
