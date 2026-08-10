package runner

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/database"
)

func TestSARIFStorePostgresBoundariesAndQueries(t *testing.T) {
	databaseURL := os.Getenv("LOREHUB_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LOREHUB_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := database.Open(ctx, databaseURL, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	fixture := newActionsFixture(t, pool)
	defer fixture.cleanup(t)
	other := newActionsFixture(t, pool)
	defer other.cleanup(t)
	runID := uuid.NewString()
	jobID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO ci_runs (
			id, repository_id, run_number, event_name, branch, revision, actor_id,
			status, event_payload, started_at
		) VALUES ($1, $2, 1, 'push', 'main', 'lore-revision-1', $3, 'in_progress', '{}', now())
	`, runID, fixture.repositoryID, fixture.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO ci_jobs (
			id, run_id, name, status, attempt, started_at, lease_owner, lease_expires_at
		) VALUES ($1, $2, 'codeql', 'in_progress', 1, now(), 'runner-test', now() + interval '5 minutes')
	`, jobID, runID); err != nil {
		t.Fatal(err)
	}

	store := NewStore(pool)
	claims := SARIFJobClaims{
		RepositoryID: fixture.repositoryID, RunID: runID, JobID: jobID, Attempt: 1,
	}
	invalidInputs := []struct {
		name  string
		input SARIFUploadInput
		want  error
	}{
		{
			name: "cross repository",
			input: sarifUploadInputForTest(SARIFJobClaims{
				RepositoryID: other.repositoryID, RunID: runID, JobID: jobID, Attempt: 1,
			}, other.owner, other.repositorySlug, validSARIFDocument("src/main.go")),
			want: ErrSARIFBoundary,
		},
		{
			name: "path repository mismatch",
			input: sarifUploadInputForTest(
				claims, other.owner, other.repositorySlug, validSARIFDocument("src/main.go")),
			want: ErrSARIFNotFound,
		},
		{
			name: "cross job",
			input: sarifUploadInputForTest(SARIFJobClaims{
				RepositoryID: fixture.repositoryID, RunID: runID, JobID: uuid.NewString(), Attempt: 1,
			}, fixture.owner, fixture.repositorySlug, validSARIFDocument("src/main.go")),
			want: ErrSARIFBoundary,
		},
		{
			name: "wrong attempt",
			input: sarifUploadInputForTest(SARIFJobClaims{
				RepositoryID: fixture.repositoryID, RunID: runID, JobID: jobID, Attempt: 2,
			}, fixture.owner, fixture.repositorySlug, validSARIFDocument("src/main.go")),
			want: ErrSARIFBoundary,
		},
		{
			name: "oversize",
			input: sarifUploadInputForTest(
				claims, fixture.owner, fixture.repositorySlug, make([]byte, MaxSARIFUploadBytes+1)),
			want: ErrSARIFTooLarge,
		},
		{
			name: "invalid",
			input: sarifUploadInputForTest(
				claims, fixture.owner, fixture.repositorySlug, []byte(`{"version":"2.1.0"}`)),
			want: ErrSARIFInvalid,
		},
		{
			name: "path escape",
			input: sarifUploadInputForTest(
				claims, fixture.owner, fixture.repositorySlug, validSARIFDocument("../outside.go")),
			want: ErrSARIFInvalid,
		},
		{
			name: "revision mismatch",
			input: SARIFUploadInput{
				Claims: claims, Owner: fixture.owner, Repository: fixture.repositorySlug,
				ExpectedRevision: "other-revision", ExpectedRef: "refs/heads/main",
				Document: validSARIFDocument("src/main.go"),
			},
			want: ErrSARIFBoundary,
		},
		{
			name: "ref mismatch",
			input: SARIFUploadInput{
				Claims: claims, Owner: fixture.owner, Repository: fixture.repositorySlug,
				ExpectedRevision: "lore-revision-1", ExpectedRef: "refs/heads/other",
				Document: validSARIFDocument("src/main.go"),
			},
			want: ErrSARIFBoundary,
		},
	}
	for _, testCase := range invalidInputs {
		t.Run(testCase.name, func(t *testing.T) {
			if _, uploadErr := store.UploadSARIF(ctx, testCase.input); !errors.Is(uploadErr, testCase.want) {
				t.Fatalf("upload error = %v, want %v", uploadErr, testCase.want)
			}
		})
	}
	var uploadCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM ci_sarif_uploads WHERE repository_id = $1
	`, fixture.repositoryID).Scan(&uploadCount); err != nil {
		t.Fatal(err)
	}
	if uploadCount != 0 {
		t.Fatalf("rejected SARIF inputs stored %d uploads", uploadCount)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE ci_jobs SET lease_expires_at = now() - interval '1 second' WHERE id = $1
	`, jobID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UploadSARIF(ctx,
		sarifUploadInputForTest(claims, fixture.owner, fixture.repositorySlug,
			validSARIFDocument("src/main.go"))); !errors.Is(err, ErrSARIFBoundary) {
		t.Fatalf("expired job lease accepted SARIF upload: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE ci_jobs SET lease_expires_at = now() + interval '5 minutes' WHERE id = $1
	`, jobID); err != nil {
		t.Fatal(err)
	}

	metadata, err := store.UploadSARIF(ctx,
		sarifUploadInputForTest(claims, fixture.owner, fixture.repositorySlug,
			validSARIFDocumentWithBase("src/main.go", "%SRCROOT%")))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.RepositoryID != fixture.repositoryID || metadata.RunID != runID || metadata.JobID != jobID ||
		metadata.Attempt != 1 || metadata.Revision != "lore-revision-1" || metadata.Ref != "refs/heads/main" ||
		metadata.ResultsCount != 1 || len(metadata.Tools) != 1 || metadata.Tools[0] != "CodeQL" ||
		metadata.CreatedAt.IsZero() {
		t.Fatalf("unexpected SARIF metadata: %#v", metadata)
	}

	selector := SARIFRepositorySelector{
		RepositoryID: fixture.repositoryID, Owner: fixture.owner, Repository: fixture.repositorySlug,
	}
	uploads, err := store.ListSARIFUploads(ctx, selector, 10)
	if err != nil || len(uploads) != 1 || uploads[0].ID != metadata.ID {
		t.Fatalf("unexpected SARIF uploads: %#v, %v", uploads, err)
	}
	loaded, err := store.GetSARIFUpload(ctx, selector, metadata.ID)
	if err != nil || loaded.ResultsCount != 1 || loaded.Revision != "lore-revision-1" {
		t.Fatalf("unexpected SARIF upload metadata: %#v, %v", loaded, err)
	}
	alerts, err := store.ListCodeScanningAlerts(ctx, selector, metadata.ID, 10)
	if err != nil || len(alerts) != 1 {
		t.Fatalf("unexpected code scanning alerts: %#v, %v", alerts, err)
	}
	alert := alerts[0]
	if alert.RepositoryID != fixture.repositoryID || alert.UploadID != metadata.ID || alert.ToolName != "CodeQL" ||
		alert.RuleID != "go/sql-injection" || alert.Level != "warning" || alert.Path != "src/main.go" ||
		alert.StartLine == nil || *alert.StartLine != 12 {
		t.Fatalf("unexpected code scanning alert: %#v", alert)
	}

	wrongOwner := selector
	wrongOwner.Owner = other.owner
	if uploads, err := store.ListSARIFUploads(ctx, wrongOwner, 10); err != nil || len(uploads) != 0 {
		t.Fatalf("owner boundary leaked SARIF uploads: %#v, %v", uploads, err)
	}
	if _, err := store.GetSARIFUpload(ctx, wrongOwner, metadata.ID); !errors.Is(err, ErrSARIFNotFound) {
		t.Fatalf("owner boundary returned SARIF metadata: %v", err)
	}

	var auditCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_events
		WHERE repository_id = $1 AND action = 'actions.code_scanning.sarif_uploaded'
		  AND target_id = $2
	`, fixture.repositoryID, metadata.ID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("SARIF upload wrote %d audit events", auditCount)
	}
	var storedVersion string
	if err := pool.QueryRow(ctx, `
		SELECT sarif->>'version' FROM ci_sarif_uploads WHERE id = $1 AND repository_id = $2
	`, metadata.ID, fixture.repositoryID).Scan(&storedVersion); err != nil {
		t.Fatal(err)
	}
	if storedVersion != "2.1.0" {
		t.Fatalf("stored SARIF version = %q", storedVersion)
	}
	if _, err := pool.Exec(ctx, `UPDATE ci_jobs SET attempt = attempt + 1 WHERE id = $1`, jobID); err != nil {
		t.Fatalf("SARIF history blocked the next job attempt: %v", err)
	}
	var storedAttempt int
	if err := pool.QueryRow(ctx, `SELECT attempt FROM ci_sarif_uploads WHERE id = $1`, metadata.ID).
		Scan(&storedAttempt); err != nil {
		t.Fatal(err)
	}
	if storedAttempt != 1 {
		t.Fatalf("stored SARIF attempt changed to %d", storedAttempt)
	}
}

func sarifUploadInputForTest(
	claims SARIFJobClaims,
	owner string,
	repository string,
	document []byte,
) SARIFUploadInput {
	return SARIFUploadInput{
		Claims: claims, Owner: owner, Repository: repository,
		ExpectedRevision: "lore-revision-1", ExpectedRef: "refs/heads/main",
		Document: document,
	}
}
