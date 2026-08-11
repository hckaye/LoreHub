package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/database"
)

func TestActionsLifecyclePostgres(t *testing.T) {
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
	logDirectory := t.TempDir()
	artifactDirectory := t.TempDir()
	store := NewStoreWithFiles(pool, logDirectory, artifactDirectory)
	repository := Repository{
		ID: fixture.repositoryID, Owner: fixture.owner, Slug: fixture.repositorySlug,
		LoreURL: "lore://fixture/repository", DefaultBranch: "main",
	}
	initial := []WorkflowDefinition{
		workflowDefinition(".github/workflows/checks.yml", "Checks", []string{"main"}, false),
		workflowDispatchDefinition(".github/workflows/manual.yml", "Manual"),
	}
	queued, err := store.ObserveBranch(ctx, repository, ObservedBranch{
		ID: "branch-main", Name: "main", LatestRevision: "rev-1",
	}, initial...)
	if err != nil {
		t.Fatal(err)
	}
	if queued {
		t.Fatal("initial branch observation invented a push run")
	}
	var runCount, workflowCount int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM ci_runs WHERE repository_id = $1", fixture.repositoryID).
		Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM ci_workflows WHERE repository_id = $1", fixture.repositoryID).
		Scan(&workflowCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 0 || workflowCount != 2 {
		t.Fatalf("unexpected initial sync counts: runs=%d workflows=%d", runCount, workflowCount)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE repository_branch_states
		SET latest_revision = 'rev-2', observed_at = now()
		WHERE repository_id = $1 AND branch_id = 'branch-main'
	`, fixture.repositoryID); err != nil {
		t.Fatal(err)
	}

	changed := []WorkflowDefinition{
		workflowDefinition(".github/workflows/checks.yml", "Checks", []string{"main"}, false),
		workflowDispatchDefinition(".github/workflows/manual.yml", "Manual"),
		{
			Path: ".github/workflows/broken.yml", Name: "Broken", Enabled: false, State: "error",
			ErrorCode: "unsupported_trigger", ErrorMessage: "workflow_run is not supported",
			TriggerConfig: json.RawMessage(`{}`),
		},
	}
	queued, err = store.ObserveBranch(ctx, repository, ObservedBranch{
		ID: "branch-main", Name: "main", LatestRevision: "rev-2",
	}, changed...)
	if err != nil {
		t.Fatal(err)
	}
	if !queued {
		t.Fatal("revision update did not queue a matching push run")
	}
	runPage, err := store.ListActionRuns(ctx, fixture.owner, fixture.repositorySlug, fixture.userID, RunFilter{
		Status: "queued", PerPage: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if runPage.Total != 1 || len(runPage.Runs) != 1 ||
		runPage.Runs[0].WorkflowPath != ".github/workflows/checks.yml" {
		t.Fatalf("unexpected workflow-specific push run: total=%d runs=%#v", runPage.Total, runPage.Runs)
	}
	pushRun := runPage.Runs[0]
	var disabled bool
	if err := pool.QueryRow(ctx, `
		SELECT state = 'disabled' FROM ci_workflows
		WHERE repository_id = $1 AND path = '.github/workflows/manual.yml'
	`, fixture.repositoryID).Scan(&disabled); err != nil {
		t.Fatal(err)
	}
	if disabled {
		t.Fatal("present workflow was disabled")
	}
	if workflowCount, err := countRepositoryRows(ctx, pool, "ci_workflows", fixture.repositoryID); err != nil {
		t.Fatal(err)
	} else if workflowCount != 3 {
		t.Fatalf("invalid workflow was not synchronized: %d workflows", workflowCount)
	}
	workflowPage, err := store.ListWorkflows(ctx, fixture.owner, fixture.repositorySlug, fixture.userID,
		PageRequest{Page: 1, PerPage: 1})
	if err != nil || len(workflowPage.Workflows) != 1 || workflowPage.Total != 3 || !workflowPage.HasMore {
		t.Fatalf("workflow pagination was incorrect: %#v, %v", workflowPage, err)
	}

	access, err := store.RepositoryForActions(ctx, fixture.owner, fixture.repositorySlug, fixture.userID)
	if err != nil || !access.CanRead || !access.CanWrite {
		t.Fatalf("unexpected repository access: %#v, %v", access, err)
	}
	if _, err := store.DispatchWorkflow(ctx, access, ".github/workflows/manual.yml", "main", "rev-2",
		map[string]string{"channel": "nightly"}, fixture.userID); !errors.Is(err, ErrActionInvalid) {
		t.Fatalf("invalid workflow dispatch input was accepted: %v", err)
	}
	dispatched, err := store.DispatchWorkflow(ctx, access, ".github/workflows/manual.yml", "main", "rev-2",
		map[string]string{"channel": "beta"}, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	runPage, err = store.ListActionRuns(ctx, fixture.owner, fixture.repositorySlug, fixture.userID, RunFilter{
		PerPage: 1,
	})
	if err != nil || len(runPage.Runs) != 1 || runPage.Total != 2 || !runPage.HasMore {
		t.Fatalf("run pagination was incorrect: %#v, %v", runPage, err)
	}
	if dispatched.EventName != "workflow_dispatch" || dispatched.RunAttempt != 1 {
		t.Fatalf("unexpected dispatch run: %#v", dispatched)
	}
	var dispatchedChannel string
	if err := pool.QueryRow(ctx, `SELECT event_payload->'inputs'->>'channel' FROM ci_runs WHERE id = $1`,
		dispatched.ID).Scan(&dispatchedChannel); err != nil {
		t.Fatal(err)
	}
	if dispatchedChannel != "beta" {
		t.Fatalf("workflow dispatch input was not retained exactly: %q", dispatchedChannel)
	}
	if _, err := store.CancelActionRun(ctx, access, dispatched.RunNumber, fixture.userID); err != nil {
		t.Fatal(err)
	}
	var cancelled string
	if err := pool.QueryRow(ctx, "SELECT status FROM ci_runs WHERE id = $1", dispatched.ID).Scan(&cancelled); err != nil {
		t.Fatal(err)
	}
	if cancelled != "cancelled" {
		t.Fatalf("dispatch cancellation was not persisted: %s", cancelled)
	}
	raceRun, err := store.DispatchWorkflow(ctx, access, ".github/workflows/manual.yml", "main", "rev-2", nil,
		fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	preloaded, err := store.ClaimJob(ctx, "preloaded-worker", time.Minute)
	if err != nil || preloaded == nil || preloaded.RunID != pushRun.ID {
		t.Fatalf("could not prepare the first queued job: job=%#v error=%v", preloaded, err)
	}
	raceJob, err := store.ClaimJob(ctx, "race-worker", time.Minute)
	if err != nil || raceJob == nil || raceJob.RunID != raceRun.ID {
		t.Fatalf("could not prepare cancellation race: job=%#v error=%v", raceJob, err)
	}
	startRace := make(chan struct{})
	finishRace := make(chan struct{}, 2)
	var cancelErr, completeErr error
	var raceWait sync.WaitGroup
	raceWait.Add(2)
	go func() {
		defer raceWait.Done()
		<-startRace
		_, cancelErr = store.CancelActionRun(ctx, access, raceRun.RunNumber, fixture.userID)
		finishRace <- struct{}{}
	}()
	go func() {
		defer raceWait.Done()
		<-startRace
		completeErr = store.CompleteJob(ctx, *raceJob, "race-worker", "success", "", nil)
		finishRace <- struct{}{}
	}()
	close(startRace)
	raceWait.Wait()
	if cancelErr == nil && completeErr == nil {
		t.Fatal("cancellation race allowed completion and cancellation to both succeed")
	}
	var raceStatus, raceConclusion string
	if err := pool.QueryRow(ctx, "SELECT status, COALESCE(conclusion, '') FROM ci_runs WHERE id = $1", raceRun.ID).
		Scan(&raceStatus, &raceConclusion); err != nil {
		t.Fatal(err)
	}
	if (raceStatus != "completed" && raceStatus != "cancelled") ||
		(raceStatus == "completed" && raceConclusion == "cancelled") ||
		(raceStatus == "cancelled" && raceConclusion != "cancelled") {
		t.Fatalf("cancellation race left an invalid aggregate state: %s/%s", raceStatus, raceConclusion)
	}

	original := pushRun
	if _, err := pool.Exec(ctx, `
		UPDATE ci_jobs SET status = 'completed', conclusion = 'success', completed_at = now()
		WHERE run_id = $1
	`, original.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE ci_runs SET status = 'completed', conclusion = 'success', completed_at = now()
		WHERE id = $1
	`, original.ID); err != nil {
		t.Fatal(err)
	}
	rerun, err := store.RerunActionRun(ctx, access, original.RunNumber, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	if rerun.RunAttempt != 2 || rerun.RerunOf == nil || *rerun.RerunOf != original.ID {
		t.Fatalf("rerun semantics were not persisted: %#v", rerun)
	}

	job, err := store.ClaimJob(ctx, "worker-a", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if job == nil {
		t.Fatal("rerun job was not claimable")
	}
	time.Sleep(10 * time.Millisecond)
	recovered, err := store.ClaimJob(ctx, "worker-b", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if recovered == nil || recovered.ID != job.ID || recovered.Attempt != 2 {
		t.Fatalf("expired lease was not recovered: first=%#v recovered=%#v", job, recovered)
	}
	if err := store.HeartbeatJob(ctx, recovered.ID, "worker-b", time.Minute); err != nil {
		t.Fatal(err)
	}
	logKey := filepath.ToSlash(filepath.Join(recovered.RepositoryID, recovered.RunID, recovered.ID, "attempt-2.log"))
	logPath := filepath.Join(logDirectory, filepath.FromSlash(logKey))
	if err := os.MkdirAll(filepath.Dir(logPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("workflow output\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.SetJobLogObjectKey(ctx, *recovered, "worker-b", logKey); err != nil {
		t.Fatal(err)
	}
	artifactKey := filepath.ToSlash(filepath.Join(
		recovered.RepositoryID, recovered.RunID, recovered.ID, "attempt-2", "output.txt",
	))
	artifactPath := filepath.Join(artifactDirectory, filepath.FromSlash(artifactKey))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteJob(ctx, *recovered, "worker-b", "success", logKey, []Artifact{{
		Name: "output.txt", ObjectKey: artifactKey, Size: int64(len("artifact")),
	}}); err != nil {
		t.Fatal(err)
	}
	detail, err := store.ActionRunDetail(ctx, fixture.owner, fixture.repositorySlug, rerun.RunNumber, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Run.Conclusion == nil || *detail.Run.Conclusion != "success" || len(detail.Artifacts) != 1 {
		t.Fatalf("completion aggregate was not persisted: %#v", detail)
	}
	logDownload, err := store.OpenActionJobLog(ctx, fixture.owner, fixture.repositorySlug, recovered.ID, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	logContents, err := io.ReadAll(logDownload.File)
	_ = logDownload.File.Close()
	if err != nil || string(logContents) != "workflow output\n" {
		t.Fatalf("unexpected job log: %q, %v", logContents, err)
	}
	artifactDownload, err := store.OpenActionArtifact(ctx, fixture.owner, fixture.repositorySlug, detail.Artifacts[0].ID,
		fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	artifactContents, err := io.ReadAll(artifactDownload.File)
	_ = artifactDownload.File.Close()
	if err != nil || string(artifactContents) != "artifact" {
		t.Fatalf("unexpected artifact: %q, %v", artifactContents, err)
	}
	if count, err := countActionEvents(ctx, pool, fixture.repositoryID); err != nil {
		t.Fatal(err)
	} else if count < 5 {
		t.Fatalf("Actions audit/outbox events were not recorded: %d", count)
	}
	if _, err := store.RepositoryForActions(ctx, fixture.owner, fixture.repositorySlug, uuid.NewString()); err == nil {
		t.Fatal("private repository was readable without membership")
	}
	if _, err := pool.Exec(ctx,
		"UPDATE repositories SET visibility = 'public' WHERE id = $1", fixture.repositoryID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListActionRuns(ctx, fixture.owner, fixture.repositorySlug, "", RunFilter{}); err != nil {
		t.Fatalf("public run metadata was not readable anonymously: %v", err)
	}
	publicLog, err := store.OpenActionJobLog(ctx, fixture.owner, fixture.repositorySlug, recovered.ID, "")
	if err != nil {
		t.Fatalf("public job log was not readable anonymously: %v", err)
	}
	_ = publicLog.File.Close()
	publicArtifact, err := store.OpenActionArtifact(ctx, fixture.owner, fixture.repositorySlug,
		detail.Artifacts[0].ID, "")
	if err != nil {
		t.Fatalf("public artifact was not readable anonymously: %v", err)
	}
	_ = publicArtifact.File.Close()
	if _, err := pool.Exec(ctx,
		"UPDATE repositories SET visibility = 'private' WHERE id = $1", fixture.repositoryID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OpenActionArtifact(ctx, fixture.owner, fixture.repositorySlug,
		detail.Artifacts[0].ID, ""); err == nil {
		t.Fatal("private artifact was readable without membership")
	}
	if _, err := store.ObserveBranch(ctx, repository, ObservedBranch{
		ID: "branch-main", Name: "main", LatestRevision: "rev-3",
	}, initial[0]); err != nil {
		t.Fatal(err)
	}
	var removed bool
	if err := pool.QueryRow(ctx, `
		SELECT state = 'disabled' FROM ci_workflows
		WHERE repository_id = $1 AND path = '.github/workflows/manual.yml'
	`, fixture.repositoryID).Scan(&removed); err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("workflow missing from a new revision was not disabled")
	}

	unsafeArtifactID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO ci_artifacts (id, job_id, name, object_key, size_bytes) VALUES ($1, $2, 'unsafe', '../../outside', 0)
	`, unsafeArtifactID, recovered.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OpenActionArtifact(ctx, fixture.owner, fixture.repositorySlug,
		unsafeArtifactID, fixture.userID); err == nil {
		t.Fatal("artifact path escape was accepted")
	}
}

func TestFeatureBranchCannotChangeActionsCatalogPostgres(t *testing.T) {
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
	store := NewStoreWithFiles(pool, t.TempDir(), t.TempDir())
	repository := Repository{
		ID: fixture.repositoryID, Owner: fixture.owner, Slug: fixture.repositorySlug,
		LoreURL: "lore://fixture/repository", DefaultBranch: "main",
	}
	canonical := workflowDefinition(".github/workflows/checks.yml", "Checks", []string{"main"}, true)
	defaultOnly := workflowDefinition(".github/workflows/default.yml", "Default", []string{"main"}, false)
	if _, err := store.ObserveBranch(ctx, repository, ObservedBranch{
		ID: "main", Name: "main", LatestRevision: "default-1",
	}, canonical, defaultOnly); err != nil {
		t.Fatal(err)
	}
	featureAdded := workflowDefinition(".github/workflows/feature.yml", "Feature", []string{"feature"}, false)
	featureInvalid := WorkflowDefinition{
		Path: ".github/workflows/invalid.yml", Name: "Invalid", Enabled: false, State: "error",
		ErrorCode: "unsupported_trigger", ErrorMessage: "workflow_run is not supported",
		TriggerConfig: json.RawMessage(`{}`),
	}
	featureCanonical := workflowDefinition(".github/workflows/checks.yml", "Feature Checks", []string{"feature"}, true)
	if queued, err := store.ObserveBranch(ctx, repository, ObservedBranch{
		ID: "feature", Name: "feature", LatestRevision: "feature-1",
	}, featureCanonical, featureAdded, featureInvalid); err != nil {
		t.Fatal(err)
	} else if queued {
		t.Fatal("initial feature observation invented a push run")
	}
	var catalogCount int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM ci_workflows WHERE repository_id = $1", fixture.repositoryID).
		Scan(&catalogCount); err != nil {
		t.Fatal(err)
	}
	if catalogCount != 2 {
		t.Fatalf("feature branch changed canonical catalog: %d", catalogCount)
	}
	featureCanonical.Name = "Feature Checks v2"
	if queued, err := store.ObserveBranch(ctx, repository, ObservedBranch{
		ID: "feature", Name: "feature", LatestRevision: "feature-2",
	}, featureCanonical, featureAdded); err != nil {
		t.Fatal(err)
	} else if !queued {
		t.Fatal("feature revision did not queue supported workflows")
	}
	var catalogName string
	if err := pool.QueryRow(ctx, `
		SELECT name FROM ci_workflows WHERE repository_id = $1 AND path = '.github/workflows/checks.yml'
	`, fixture.repositoryID).Scan(&catalogName); err != nil {
		t.Fatal(err)
	}
	if catalogName != "Checks" {
		t.Fatalf("feature workflow overwrote canonical name: %q", catalogName)
	}
	page, err := store.ListActionRuns(ctx, fixture.owner, fixture.repositorySlug, fixture.userID, RunFilter{
		PerPage: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Runs) != 2 {
		t.Fatalf("feature workflow runs were not independent: %#v", page)
	}
	var revisionRunCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM ci_runs WHERE repository_id = $1 AND workflow_revision_id IS NOT NULL
	`, fixture.repositoryID).Scan(&revisionRunCount); err != nil {
		t.Fatal(err)
	}
	if revisionRunCount != 2 {
		t.Fatalf("feature runs did not retain exact workflow revisions: %d", revisionRunCount)
	}
	var featureCanonicalID, featureRevisionID *string
	if err := pool.QueryRow(ctx, `
		SELECT workflow_id::text, workflow_revision_id::text
		FROM ci_runs
		WHERE repository_id = $1 AND branch = 'feature'
		ORDER BY run_number
		LIMIT 1
	`, fixture.repositoryID).Scan(&featureCanonicalID, &featureRevisionID); err != nil {
		t.Fatal(err)
	}
	if featureCanonicalID != nil || featureRevisionID == nil {
		t.Fatalf("feature run linked to the canonical catalog: workflow=%v revision=%v",
			featureCanonicalID, featureRevisionID)
	}
	access, err := store.RepositoryForActions(ctx, fixture.owner, fixture.repositorySlug, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DispatchWorkflow(ctx, access, featureAdded.Path, "feature", "feature-2", nil,
		fixture.userID); err != ErrActionNotFound {
		t.Fatalf("feature-only workflow was dispatchable from catalog: %v", err)
	}
	var featureInvalidState string
	if err := pool.QueryRow(ctx, `
		SELECT state FROM ci_workflow_revisions
		WHERE repository_id = $1 AND revision = 'feature-1' AND path = $2
	`, fixture.repositoryID, featureInvalid.Path).Scan(&featureInvalidState); err != nil {
		t.Fatal(err)
	}
	if featureInvalidState != "error" {
		t.Fatalf("invalid feature workflow was not retained as an error: %q", featureInvalidState)
	}
	defaultInvalid := WorkflowDefinition{
		Path: ".github/workflows/default-invalid.yml", Name: "Default invalid", Enabled: false, State: "error",
		ErrorCode: "unsupported_trigger", ErrorMessage: "workflow_run is not supported",
		TriggerConfig: json.RawMessage(`{}`),
	}
	if _, err := store.ObserveBranch(ctx, repository, ObservedBranch{
		ID: "main", Name: "main", LatestRevision: "default-2",
	}, canonical, defaultInvalid); err != nil {
		t.Fatal(err)
	}
	var disabled bool
	if err := pool.QueryRow(ctx, `
		SELECT state = 'disabled' FROM ci_workflows
		WHERE repository_id = $1 AND path = '.github/workflows/default.yml'
	`, fixture.repositoryID).Scan(&disabled); err != nil {
		t.Fatal(err)
	}
	if !disabled {
		t.Fatal("default branch removal did not disable canonical workflow")
	}
	var defaultInvalidState string
	if err := pool.QueryRow(ctx, `
		SELECT state FROM ci_workflows
		WHERE repository_id = $1 AND path = $2
	`, fixture.repositoryID, defaultInvalid.Path).Scan(&defaultInvalidState); err != nil {
		t.Fatal(err)
	}
	if defaultInvalidState != "error" {
		t.Fatalf("invalid default workflow was not retained as an error: %q", defaultInvalidState)
	}
}

func TestActionsSupportedEventsAndScheduleDeduplicationPostgres(t *testing.T) {
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
	store := NewStore(pool)
	repository := Repository{
		ID: fixture.repositoryID, Owner: fixture.owner, Slug: fixture.repositorySlug,
		LoreURL: "lore://fixture/repository", DefaultBranch: "main",
	}
	scheduled := workflowWithTriggerConfig(
		".github/workflows/scheduled.yml", "Scheduled", map[string]any{
			"schedule": []ScheduleTrigger{{Cron: "*/15 * * * *"}},
		},
	)
	dispatch := workflowWithTriggerConfig(
		".github/workflows/dispatch.yml", "Dispatch", map[string]any{
			"repository_dispatch": &RepositoryDispatchTrigger{Types: []string{"refresh"}},
		},
	)
	pullRequest := workflowWithTriggerConfig(
		".github/workflows/pull-request.yml", "Pull request", map[string]any{
			"pull_request": &PullRequestTrigger{Branches: []string{"main"}, Types: []string{"opened"}},
		},
	)
	if queued, err := store.ObserveBranch(ctx, repository, ObservedBranch{
		ID: "main", Name: "main", LatestRevision: "revision-main",
	}, scheduled, dispatch, pullRequest); err != nil {
		t.Fatal(err)
	} else if queued {
		t.Fatal("initial event catalog observation invented a push")
	}
	now := time.Date(2026, time.August, 9, 12, 31, 0, 0, time.UTC)
	runs, err := store.EnqueueScheduledRuns(ctx, repository, "revision-main", now)
	if err != nil || len(runs) != 1 || runs[0].EventName != "schedule" {
		t.Fatalf("schedule was not enqueued: %#v, %v", runs, err)
	}
	if duplicate, err := store.EnqueueScheduledRuns(ctx, repository, "revision-main", now); err != nil {
		t.Fatal(err)
	} else if len(duplicate) != 0 {
		t.Fatalf("schedule occurrence was duplicated: %#v", duplicate)
	}
	access, err := store.RepositoryForActions(ctx, fixture.owner, fixture.repositorySlug, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	runs, err = store.DispatchRepositoryEvent(ctx, access, RepositoryDispatchEvent{
		EventType: "refresh", Branch: "main", Revision: "revision-dispatch",
		ClientPayload: json.RawMessage(`{"source":"test"}`),
	}, fixture.userID)
	if err != nil || len(runs) != 1 || runs[0].EventName != "repository_dispatch" {
		t.Fatalf("repository_dispatch was not enqueued: %#v, %v", runs, err)
	}
	var dispatchType, dispatchSource string
	if err := pool.QueryRow(ctx, `
		SELECT event_payload->>'event_type', event_payload->'client_payload'->>'source'
		FROM ci_runs WHERE repository_id = $1 AND event_name = 'repository_dispatch'
	`, fixture.repositoryID).Scan(&dispatchType, &dispatchSource); err != nil {
		t.Fatal(err)
	}
	if dispatchType != "refresh" || dispatchSource != "test" {
		t.Fatalf("repository dispatch event fields were not retained: %q %q", dispatchType, dispatchSource)
	}
	runs, err = store.EnqueuePullRequest(ctx, access, PullRequestEvent{
		Action: "opened", Number: 7, SourceBranch: "feature", TargetBranch: "main",
		SourceRevision: "revision-source", TargetRevision: "revision-target",
	}, fixture.userID)
	if err != nil || len(runs) != 1 || runs[0].EventName != "pull_request" ||
		runs[0].Revision != "revision-source" || runs[0].Branch != "main" {
		t.Fatalf("pull_request was not enqueued with exact revisions: %#v, %v", runs, err)
	}
	var sourceRevision, targetRevision string
	if err := pool.QueryRow(ctx, `
		SELECT event_payload->'pull_request'->'head'->>'sha', event_payload->'pull_request'->'base'->>'sha'
		FROM ci_runs WHERE repository_id = $1 AND event_name = 'pull_request'
	`, fixture.repositoryID).Scan(&sourceRevision, &targetRevision); err != nil {
		t.Fatal(err)
	}
	if sourceRevision != "revision-source" || targetRevision != "revision-target" {
		t.Fatalf("pull request source and target revisions were not retained: %q %q", sourceRevision, targetRevision)
	}
}

func TestActionsPermissionMatrixPostgres(t *testing.T) {
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
	store := NewStore(pool)
	readUser := uuid.NewString()
	writeUser := uuid.NewString()
	teamUser := uuid.NewString()
	maintainUser := uuid.NewString()
	orgMember := uuid.NewString()
	suspendedUser := uuid.NewString()
	inactiveUser := uuid.NewString()
	inactiveRepositoryUser := uuid.NewString()
	inactiveTeamUser := uuid.NewString()
	inactiveOrgUser := uuid.NewString()
	creatorOnlyUser := uuid.NewString()
	for _, userID := range []string{
		readUser, writeUser, teamUser, maintainUser, orgMember, suspendedUser, inactiveUser,
		inactiveRepositoryUser, inactiveTeamUser, inactiveOrgUser, creatorOnlyUser,
	} {
		status := "active"
		if userID == suspendedUser {
			status = "suspended"
		} else if userID == inactiveUser {
			status = "inactive"
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO users (id, username, display_name, status)
			VALUES ($1, $2, 'Actions Matrix', $3)
		`, userID, "matrix-"+strings.ToLower(uuid.NewString()[:8]), status)
		if err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1::uuid[])`,
			[]string{readUser, writeUser, teamUser, orgMember, suspendedUser, inactiveUser,
				inactiveRepositoryUser, inactiveTeamUser, inactiveOrgUser, maintainUser, creatorOnlyUser})
	}()
	if _, err := pool.Exec(ctx, `
		INSERT INTO repository_memberships (repository_id, user_id, role) VALUES
		($1, $2, 'read'), ($1, $3, 'write'), ($1, $4, 'admin'), ($1, $5, 'read'),
		($1, $6, 'maintain')
	`, fixture.repositoryID, readUser, writeUser, suspendedUser, inactiveRepositoryUser, maintainUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE repository_memberships SET active = false WHERE repository_id = $1 AND user_id = $2
	`, fixture.repositoryID, inactiveRepositoryUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'maintainer'), ($1, $3, 'member'), ($1, $4, 'member'), ($1, $5, 'member'),
		       ($1, $6, 'member'), ($1, $7, 'member'), ($1, $8, 'member')
	`, fixture.organizationID, orgMember, teamUser, inactiveTeamUser, inactiveOrgUser,
		readUser, writeUser, maintainUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE organization_memberships SET active = false WHERE organization_id = $1 AND user_id = $2
	`, fixture.organizationID, inactiveOrgUser); err != nil {
		t.Fatal(err)
	}
	teamID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO teams (id, organization_id, slug, display_name, created_by)
		VALUES ($1, $2, 'actions-team', 'Actions Team', $3)
	`, teamID, fixture.organizationID, fixture.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO team_memberships (team_id, user_id, role) VALUES
		($1, $2, 'member'), ($1, $3, 'member')
	`, teamID, teamUser, inactiveTeamUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE team_memberships SET active = false WHERE team_id = $1 AND user_id = $2
	`, teamID, inactiveTeamUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO team_repository_roles (team_id, repository_id, role, created_by)
		VALUES ($1, $2, 'maintain', $3)
	`, teamID, fixture.repositoryID, fixture.userID); err != nil {
		t.Fatal(err)
	}
	privateCases := []struct {
		name     string
		actorID  string
		read     bool
		write    bool
		notFound bool
	}{
		{name: "anonymous private", actorID: "", notFound: true},
		{name: "outsider private", actorID: uuid.NewString(), notFound: true},
		{name: "direct read", actorID: readUser, read: true},
		{name: "direct write", actorID: writeUser, read: true, write: true},
		{name: "direct maintain", actorID: maintainUser, read: true, write: true},
		{name: "team write", actorID: teamUser, read: true, write: true},
		{name: "active org member private", actorID: orgMember, notFound: true},
		{name: "suspended direct member", actorID: suspendedUser, notFound: true},
		{name: "inactive user", actorID: inactiveUser, notFound: true},
		{name: "inactive repository membership", actorID: inactiveRepositoryUser, notFound: true},
		{name: "inactive team membership", actorID: inactiveTeamUser, notFound: true},
		{name: "inactive organization membership", actorID: inactiveOrgUser, notFound: true},
		{name: "repository owner", actorID: fixture.userID, read: true, write: true},
	}
	for _, testCase := range privateCases {
		access, accessErr := store.RepositoryForActions(ctx, fixture.owner, fixture.repositorySlug, testCase.actorID)
		if testCase.notFound {
			if !errors.Is(accessErr, ErrActionNotFound) {
				t.Fatalf("%s: error=%v, want 404-equivalent", testCase.name, accessErr)
			}
			continue
		}
		if accessErr != nil || access.CanRead != testCase.read || access.CanWrite != testCase.write {
			t.Fatalf("%s: access=%#v error=%v", testCase.name, access, accessErr)
		}
	}
	if _, err := pool.Exec(ctx, `
		UPDATE organizations SET created_by = $2 WHERE id = $1
	`, fixture.organizationID, creatorOnlyUser); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE repositories SET created_by = $2 WHERE id = $1
	`, fixture.repositoryID, creatorOnlyUser); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RepositoryForActions(ctx, fixture.owner, fixture.repositorySlug, creatorOnlyUser); !errors.Is(
		err, ErrActionNotFound,
	) {
		t.Fatalf("organization/repository creator received permanent Actions access: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE organizations SET created_by = $2 WHERE id = $1
	`, fixture.organizationID, fixture.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE repositories SET created_by = $2 WHERE id = $1
	`, fixture.repositoryID, fixture.userID); err != nil {
		t.Fatal(err)
	}
	internalSlug := "internal-" + strings.ToLower(uuid.NewString()[:8])
	internalID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, visibility, lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Internal', 'internal', $4, $5, 'main', $6)
		`, internalID, fixture.organizationID, internalSlug, strings.ReplaceAll(internalID, "-", ""),
		"lore://"+internalID, fixture.userID); err != nil {
		t.Fatal(err)
	}
	access, err := store.RepositoryForActions(ctx, fixture.owner, internalSlug, "")
	if !errors.Is(err, ErrActionNotFound) || access.CanRead {
		t.Fatalf("anonymous internal repository was readable: %#v, %v", access, err)
	}
	access, err = store.RepositoryForActions(ctx, fixture.owner, internalSlug, orgMember)
	if err != nil || !access.CanRead || access.CanWrite {
		t.Fatalf("active org member internal access was wrong: %#v, %v", access, err)
	}
	access, err = store.RepositoryForActions(ctx, fixture.owner, internalSlug, inactiveOrgUser)
	if !errors.Is(err, ErrActionNotFound) || access.CanRead {
		t.Fatalf("inactive org member internal access was readable: %#v, %v", access, err)
	}
	publicSlug := "public-" + strings.ToLower(uuid.NewString()[:8])
	publicID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, visibility, lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Public', 'public', $4, $5, 'main', $6)
	`, publicID, fixture.organizationID, publicSlug, strings.ReplaceAll(publicID, "-", ""),
		"lore://"+publicID, fixture.userID); err != nil {
		t.Fatal(err)
	}
	access, err = store.RepositoryForActions(ctx, fixture.owner, publicSlug, "")
	if err != nil || !access.CanRead || access.CanWrite {
		t.Fatalf("anonymous public access was wrong: %#v, %v", access, err)
	}
	_, err = store.RepositoryForActions(ctx, fixture.owner, publicSlug, suspendedUser)
	if !errors.Is(err, ErrActionNotFound) {
		t.Fatalf("suspended user fell back to anonymous public access: %v", err)
	}
	_, err = store.RepositoryForActions(ctx, fixture.owner, publicSlug, inactiveUser)
	if !errors.Is(err, ErrActionNotFound) {
		t.Fatalf("inactive user fell back to anonymous public access: %v", err)
	}
}
