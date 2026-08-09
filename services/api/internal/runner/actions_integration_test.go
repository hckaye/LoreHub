package runner

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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
		ID: fixture.repositoryID, Owner: fixture.owner, Slug: fixture.repositorySlug, LoreURL: "lore://fixture/repository",
	}
	initial := []WorkflowDefinition{
		workflowDefinition(".github/workflows/checks.yml", "Checks", []string{"main"}, false),
		workflowDefinition(".github/workflows/manual.yml", "Manual", nil, true),
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

	changed := []WorkflowDefinition{
		workflowDefinition(".github/workflows/checks.yml", "Checks", []string{"main"}, false),
		workflowDefinition(".github/workflows/manual.yml", "Manual", nil, true),
		{
			Path: ".github/workflows/broken.yml", Name: "Broken", Enabled: false, State: "error",
			ErrorCode: "unsupported_trigger", ErrorMessage: "pull_request is not supported",
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
	runs, total, err := store.ListActionRuns(ctx, fixture.owner, fixture.repositorySlug, fixture.userID, RunFilter{
		Status: "queued", PerPage: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(runs) != 1 || runs[0].WorkflowPath != ".github/workflows/checks.yml" {
		t.Fatalf("unexpected workflow-specific push run: total=%d runs=%#v", total, runs)
	}
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

	access, err := store.RepositoryForActions(ctx, fixture.owner, fixture.repositorySlug, fixture.userID)
	if err != nil || !access.CanRead || !access.CanWrite {
		t.Fatalf("unexpected repository access: %#v, %v", access, err)
	}
	payload := []byte(`{"ref":"refs/heads/main","inputs":{}}`)
	dispatched, err := store.DispatchWorkflow(ctx, access, ".github/workflows/manual.yml", "main", "rev-2", payload,
		fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	if dispatched.EventName != "workflow_dispatch" || dispatched.RunAttempt != 1 {
		t.Fatalf("unexpected dispatch run: %#v", dispatched)
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
	raceRun, err := store.DispatchWorkflow(ctx, access, ".github/workflows/manual.yml", "main", "rev-2", payload,
		fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	preloaded, err := store.ClaimJob(ctx, "preloaded-worker", time.Minute)
	if err != nil || preloaded == nil || preloaded.RunID != runs[0].ID {
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

	original := runs[0]
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
	if _, _, err := store.ListActionRuns(ctx, fixture.owner, fixture.repositorySlug, "", RunFilter{}); err != nil {
		t.Fatalf("public run metadata was not readable anonymously: %v", err)
	}
	publicLog, err := store.OpenActionJobLog(ctx, fixture.owner, fixture.repositorySlug, recovered.ID, "")
	if err != nil {
		t.Fatalf("public job log was not readable anonymously: %v", err)
	}
	_ = publicLog.File.Close()
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

func countRepositoryRows(ctx context.Context, pool *pgxpool.Pool, table string, repositoryID string) (int, error) {
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+table+" WHERE repository_id = $1", repositoryID).
		Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func countActionEvents(ctx context.Context, pool *pgxpool.Pool, repositoryID string) (int, error) {
	var auditCount, outboxCount int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_events WHERE repository_id = $1 AND action LIKE 'actions.%'",
		repositoryID).Scan(&auditCount); err != nil {
		return 0, err
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM outbox_events WHERE topic LIKE 'actions.%'").
		Scan(&outboxCount); err != nil {
		return 0, err
	}
	return auditCount + outboxCount, nil
}

type actionsFixture struct {
	pool           *pgxpool.Pool
	userID         string
	organizationID string
	repositoryID   string
	owner          string
	repositorySlug string
}

func newActionsFixture(t *testing.T, pool *pgxpool.Pool) actionsFixture {
	t.Helper()
	fixture := actionsFixture{
		pool:           pool,
		userID:         uuid.NewString(),
		organizationID: uuid.NewString(),
		repositoryID:   uuid.NewString(),
		owner:          "actions-test-" + strings.ToLower(uuid.NewString()[:8]),
		repositorySlug: "runtime-" + strings.ToLower(uuid.NewString()[:8]),
	}
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, display_name) VALUES ($1, $2, 'Actions Test')
	`, fixture.userID, "actions-user-"+strings.ToLower(uuid.NewString()[:8]))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO organizations (id, slug, display_name, created_by)
		VALUES ($1, $2, 'Actions Test', $3)
	`, fixture.organizationID, fixture.owner, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'owner')
	`, fixture.organizationID, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Runtime', $5, $6, 'main', $4)
	`, fixture.repositoryID, fixture.organizationID, fixture.repositorySlug, fixture.userID,
		"lore-"+fixture.repositoryID, "lore://fixture/"+fixture.repositoryID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO repository_memberships (repository_id, user_id, role) VALUES ($1, $2, 'admin')
	`, fixture.repositoryID, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, fixture.repositoryID)
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture actionsFixture) cleanup(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := fixture.pool.Exec(ctx, "DELETE FROM organizations WHERE id = $1", fixture.organizationID); err != nil {
		t.Error(err)
	}
	if _, err := fixture.pool.Exec(ctx, "DELETE FROM users WHERE id = $1", fixture.userID); err != nil {
		t.Error(err)
	}
}

func workflowDefinition(path string, name string, branches []string, dispatch bool) WorkflowDefinition {
	config := map[string]any{}
	if branches != nil {
		config["push"] = map[string]any{"branches": branches}
	}
	if dispatch {
		config["workflow_dispatch"] = map[string]any{}
	}
	encoded, _ := json.Marshal(config)
	return WorkflowDefinition{
		Path: path, Name: name, Enabled: true, State: "active", Push: triggerFromBranches(branches),
		WorkflowDispatch: dispatch, TriggerConfig: encoded,
	}
}

func triggerFromBranches(branches []string) *PushTrigger {
	if branches == nil {
		return nil
	}
	return &PushTrigger{Branches: branches}
}
