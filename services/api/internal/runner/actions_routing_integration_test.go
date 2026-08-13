package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/database"
)

func TestActionsRoutingAndManagedEntitlementPostgres(t *testing.T) {
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
	managed := workflowWithTriggerConfig(
		".github/workflows/managed.yml", "Managed", map[string]any{
			"workflow_dispatch": map[string]any{}, "runner_labels": []string{"ubuntu-latest"},
		},
	)
	managed.WorkflowDispatch = true
	selfHosted := workflowWithTriggerConfig(
		".github/workflows/self-hosted.yml", "Self-hosted", map[string]any{
			"workflow_dispatch": map[string]any{},
			"runner_labels":     []string{"linux", "self-hosted", "x64"},
		},
	)
	selfHosted.WorkflowDispatch = true
	if _, err := store.ObserveBranch(ctx, repository, ObservedBranch{
		ID: "main", Name: "main", LatestRevision: "revision-1",
	}, managed, selfHosted); err != nil {
		t.Fatal(err)
	}
	access, err := store.RepositoryForActions(ctx, fixture.owner, fixture.repositorySlug, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	managedRun, err := store.DispatchWorkflow(
		ctx, access, managed.Path, "main", "revision-1", nil, fixture.userID,
	)
	if err != nil {
		t.Fatal(err)
	}
	selfHostedRun, err := store.DispatchWorkflow(
		ctx, access, selfHosted.Path, "main", "revision-1", nil, fixture.userID,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []struct {
		runID  string
		target string
		labels []string
	}{
		{runID: managedRun.ID, target: "managed", labels: []string{"ubuntu-latest"}},
		{runID: selfHostedRun.ID, target: "self_hosted", labels: []string{"linux", "self-hosted", "x64"}},
	} {
		var target string
		var labelsJSON []byte
		if err := pool.QueryRow(ctx, `
			SELECT execution_target, runner_labels FROM ci_jobs WHERE run_id = $1
		`, expected.runID).Scan(&target, &labelsJSON); err != nil {
			t.Fatal(err)
		}
		var labels []string
		if err := json.Unmarshal(labelsJSON, &labels); err != nil {
			t.Fatal(err)
		}
		if target != expected.target || !equalRunnerLabels(labels, expected.labels) {
			t.Fatalf("run %s routing=%q labels=%v", expected.runID, target, labels)
		}
	}
	if _, err := pool.Exec(ctx, `
		UPDATE entitlements SET revoked_at = now()
		WHERE organization_id = $1 AND feature = 'hosted_runners' AND revoked_at IS NULL
	`, fixture.organizationID); err != nil {
		t.Fatal(err)
	}
	failed, err := store.DispatchWorkflow(
		ctx, access, managed.Path, "main", "revision-1", nil, fixture.userID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != "completed" || failed.Conclusion == nil || *failed.Conclusion != "failure" ||
		failed.FailureReason == nil || *failed.FailureReason != "entitlement_required" {
		t.Fatalf("managed run without entitlement was not recorded as failed: %+v", failed)
	}
}

func TestRunnerClaimFiltersTargetLabelsAndScopePostgres(t *testing.T) {
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
	managedJobID := insertRoutingJob(t, pool, fixture, "managed", []string{"ubuntu-latest"})
	selfHostedJobID := insertRoutingJob(t, pool, fixture, "self_hosted", []string{"self-hosted", "linux"})
	digest := bytes.Repeat([]byte{7}, 32)
	runnerID := insertRoutingRunner(t, pool, fixture, digest, []string{
		"self-hosted", "linux", "ubuntu-latest",
	})
	claimed, err := store.RunnerClaimJob(ctx, digest, "routing-test-key", time.Now().UTC(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != selfHostedJobID {
		t.Fatalf("runner claimed wrong target: %+v; managed=%s", claimed, managedJobID)
	}
	var leasedRunnerID string
	if err := pool.QueryRow(ctx, `SELECT runner_id FROM ci_jobs WHERE id = $1`, claimed.ID).
		Scan(&leasedRunnerID); err != nil || leasedRunnerID != runnerID {
		t.Fatalf("runner lease was not recorded: %q, %v", leasedRunnerID, err)
	}
	otherRunnerID := uuid.NewString()
	if _, err := store.RunnerLeaseJob(ctx, claimed.ID, otherRunnerID); !errors.Is(err, ErrRunnerLeaseNotHeld) {
		t.Fatalf("non-leaseholder read runner job: %v", err)
	}
	err = store.RunnerHeartbeatJob(ctx, claimed.ID, otherRunnerID, time.Minute)
	if !errors.Is(err, ErrRunnerLeaseNotHeld) {
		t.Fatalf("non-leaseholder heartbeated runner job: %v", err)
	}
	if _, err = store.RunnerCancellationRequested(ctx, claimed.ID, otherRunnerID); !errors.Is(err, ErrRunnerLeaseNotHeld) {
		t.Fatalf("non-leaseholder polled runner job cancellation: %v", err)
	}
	if err := store.RunnerHeartbeatJob(ctx, claimed.ID, runnerID, time.Minute); err != nil {
		t.Fatalf("leaseholder could not heartbeat runner job: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE ci_runs SET status = 'cancelled', conclusion = 'cancelled', cancel_requested = true
		WHERE id = $1
	`, claimed.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE ci_jobs SET status = 'cancelled', conclusion = 'cancelled',
		  lease_owner = NULL, lease_expires_at = NULL WHERE id = $1
	`, claimed.ID); err != nil {
		t.Fatal(err)
	}
	if requested, err := store.RunnerCancellationRequested(ctx, claimed.ID, runnerID); err != nil || !requested {
		t.Fatalf("leaseholder could not observe cancellation: requested=%v err=%v", requested, err)
	}
	if managed, err := store.ClaimJob(ctx, "managed-worker", time.Minute); err != nil ||
		managed == nil || managed.ID != managedJobID {
		t.Fatalf("managed worker did not claim managed job: %+v, %v", managed, err)
	}

	labelJobID := insertRoutingJob(t, pool, fixture, "self_hosted", []string{"self-hosted", "gpu"})
	if claimed, err := store.RunnerClaimJob(ctx, digest, "routing-test-key", time.Now().UTC(), time.Minute); err != nil {
		t.Fatal(err)
	} else if claimed != nil {
		t.Fatalf("runner claimed job with missing label %s: %+v", labelJobID, claimed)
	}
	other := newActionsFixture(t, pool)
	defer other.cleanup(t)
	otherJobID := insertRoutingJob(t, pool, other, "self_hosted", []string{"self-hosted", "linux"})
	if claimed, err := store.RunnerClaimJob(ctx, digest, "routing-test-key", time.Now().UTC(), time.Minute); err != nil {
		t.Fatal(err)
	} else if claimed != nil {
		t.Fatalf("runner crossed organization scope to job %s: %+v", otherJobID, claimed)
	}
}

func TestPerJobSchedulingRoutesDependenciesAndAggregatesPostgres(t *testing.T) {
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
	workflow := perJobWorkflowDefinition(".github/workflows/mixed.yml", []WorkflowJobDefinition{
		{JobName: "build", RunnerLabels: []string{"ubuntu-latest"}, Needs: []string{}},
		{JobName: "package", RunnerLabels: []string{"linux", "self-hosted"}, Needs: []string{}},
		{
			JobName: "test", RunnerLabels: []string{"ubuntu-latest"},
			Needs: []string{"build", "package"},
		},
	})
	if _, err := store.ObserveBranch(ctx, repository, ObservedBranch{
		ID: "main", Name: "main", LatestRevision: "revision-1",
	}, workflow); err != nil {
		t.Fatal(err)
	}
	access, err := store.RepositoryForActions(ctx, fixture.owner, fixture.repositorySlug, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.DispatchWorkflow(ctx, access, workflow.Path, "main", "revision-1", nil, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}

	type scheduledJob struct {
		target string
		labels []string
		needs  []string
	}
	jobs := make(map[string]scheduledJob)
	rows, err := pool.Query(ctx, `
		SELECT job_name, execution_target, runner_labels, needs
		FROM ci_jobs WHERE run_id = $1
	`, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var jobName, target string
		var labelsJSON, needsJSON []byte
		if err := rows.Scan(&jobName, &target, &labelsJSON, &needsJSON); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		var labels, needs []string
		if err := json.Unmarshal(labelsJSON, &labels); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if err := json.Unmarshal(needsJSON, &needs); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		jobs[jobName] = scheduledJob{target: target, labels: labels, needs: needs}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 3 || jobs["build"].target != "managed" ||
		!equalRunnerLabels(jobs["build"].labels, []string{"ubuntu-latest"}) ||
		jobs["package"].target != "self_hosted" ||
		!equalRunnerLabels(jobs["package"].labels, []string{"linux", "self-hosted"}) ||
		!equalRunnerLabels(jobs["test"].needs, []string{"build", "package"}) {
		t.Fatalf("unexpected per-job scheduling rows: %#v", jobs)
	}

	digest := bytes.Repeat([]byte{9}, 32)
	runnerID := insertRoutingRunner(t, pool, fixture, digest, []string{"linux", "self-hosted"})
	managedJob, err := store.ClaimJob(ctx, "managed-worker", time.Minute)
	if err != nil || managedJob == nil || managedJob.JobName != "build" {
		t.Fatalf("managed build job was not routed correctly: %#v, %v", managedJob, err)
	}
	selfHostedJob, err := store.RunnerClaimJob(
		ctx, digest, "routing-test-key", time.Now().UTC(), time.Minute,
	)
	if err != nil || selfHostedJob == nil || selfHostedJob.JobName != "package" {
		t.Fatalf("self-hosted package job was not routed correctly: %#v, %v", selfHostedJob, err)
	}
	if err := store.CompleteJob(ctx, *managedJob, "managed-worker", "success", "", nil); err != nil {
		t.Fatal(err)
	}
	if blocked, err := store.ClaimJob(ctx, "blocked-worker", time.Minute); err != nil || blocked != nil {
		t.Fatalf("dependent job was claimable before all needs succeeded: %#v, %v", blocked, err)
	}
	if err := store.CompleteJob(ctx, *selfHostedJob, runnerID, "success", "", nil); err != nil {
		t.Fatal(err)
	}
	dependent, err := store.ClaimJob(ctx, "dependent-worker", time.Minute)
	if err != nil || dependent == nil || dependent.JobName != "test" {
		t.Fatalf("dependent job did not become claimable: %#v, %v", dependent, err)
	}
	if err := store.CompleteJob(ctx, *dependent, "dependent-worker", "success", "", nil); err != nil {
		t.Fatal(err)
	}
	var runStatus, runConclusion string
	if err := pool.QueryRow(ctx, `
		SELECT status, COALESCE(conclusion, '') FROM ci_runs WHERE id = $1
	`, run.ID).Scan(&runStatus, &runConclusion); err != nil {
		t.Fatal(err)
	}
	if runStatus != "completed" || runConclusion != "success" {
		t.Fatalf("multi-job run was not aggregated: %s/%s", runStatus, runConclusion)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE entitlements SET revoked_at = now()
		WHERE organization_id = $1 AND feature = 'hosted_runners' AND revoked_at IS NULL
	`, fixture.organizationID); err != nil {
		t.Fatal(err)
	}
	failedRun, err := store.DispatchWorkflow(
		ctx, access, workflow.Path, "main", "revision-1", nil, fixture.userID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if failedRun.Status != "completed" || failedRun.Conclusion == nil || *failedRun.Conclusion != "failure" ||
		failedRun.FailureReason == nil || *failedRun.FailureReason != "entitlement_required" {
		t.Fatalf("mixed run without managed entitlement did not fail: %#v", failedRun)
	}
	var failedJobs, cancelledJobs int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE conclusion = 'failure'),
		       COUNT(*) FILTER (WHERE conclusion = 'cancelled')
		FROM ci_jobs WHERE run_id = $1
	`, failedRun.ID).Scan(&failedJobs, &cancelledJobs); err != nil {
		t.Fatal(err)
	}
	if failedJobs != 1 || cancelledJobs != 2 {
		t.Fatalf("entitlement failure did not cancel sibling jobs: failure=%d cancelled=%d", failedJobs, cancelledJobs)
	}
}

func TestFailedJobSkipsDependentJobsPostgres(t *testing.T) {
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
	workflow := perJobWorkflowDefinition(".github/workflows/failure.yml", []WorkflowJobDefinition{
		{JobName: "build", RunnerLabels: []string{"ubuntu-latest"}, Needs: []string{}},
		{JobName: "test", RunnerLabels: []string{"ubuntu-latest"}, Needs: []string{"build"}},
		{JobName: "deploy", RunnerLabels: []string{"ubuntu-latest"}, Needs: []string{"test"}},
	})
	repository := Repository{
		ID: fixture.repositoryID, Owner: fixture.owner, Slug: fixture.repositorySlug,
		LoreURL: "lore://fixture/repository", DefaultBranch: "main",
	}
	if _, err := store.ObserveBranch(ctx, repository, ObservedBranch{
		ID: "main", Name: "main", LatestRevision: "revision-1",
	}, workflow); err != nil {
		t.Fatal(err)
	}
	access, err := store.RepositoryForActions(ctx, fixture.owner, fixture.repositorySlug, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.DispatchWorkflow(ctx, access, workflow.Path, "main", "revision-1", nil, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	build, err := store.ClaimJob(ctx, "failure-worker", time.Minute)
	if err != nil || build == nil || build.JobName != "build" {
		t.Fatalf("root job was not claimable: %#v, %v", build, err)
	}
	if err := store.CompleteJob(ctx, *build, "failure-worker", "failure", "", nil); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `
		SELECT job_name, status, COALESCE(conclusion, '')
		FROM ci_jobs WHERE run_id = $1 ORDER BY job_name
	`, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	statuses := make(map[string]string)
	for rows.Next() {
		var jobName, status, conclusion string
		if err := rows.Scan(&jobName, &status, &conclusion); err != nil {
			t.Fatal(err)
		}
		statuses[jobName] = status + "/" + conclusion
	}
	if statuses["build"] != "completed/failure" || statuses["test"] != "completed/skipped" ||
		statuses["deploy"] != "completed/skipped" {
		t.Fatalf("failed dependency did not skip dependents: %#v", statuses)
	}
	var status, conclusion string
	if err := pool.QueryRow(ctx, `
		SELECT status, COALESCE(conclusion, '') FROM ci_runs WHERE id = $1
	`, run.ID).Scan(&status, &conclusion); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || conclusion != "failure" {
		t.Fatalf("failed multi-job run was not aggregated: %s/%s", status, conclusion)
	}
	if claimed, err := store.ClaimJob(ctx, "unexpected-worker", time.Minute); err != nil || claimed != nil {
		t.Fatalf("skipped dependent remained claimable: %#v, %v", claimed, err)
	}
}

func perJobWorkflowDefinition(path string, jobs []WorkflowJobDefinition) WorkflowDefinition {
	config := map[string]any{"workflow_dispatch": map[string]any{}, "jobs": jobs}
	workflow := workflowWithTriggerConfig(path, "Per-job", config)
	workflow.WorkflowDispatch = true
	workflow.Jobs = jobs
	return workflow
}

func insertRoutingRunner(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture actionsFixture,
	digest []byte,
	labels []string,
) string {
	t.Helper()
	runnerID := uuid.NewString()
	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO ci_runners (
			id, organization_id, name, labels, credential_digest, credential_key_id,
			credential_expires_at, runner_version
		) VALUES ($1, $2, 'Routing runner', $3, $4, 'routing-test-key',
		          now() + interval '1 day', 'test')
	`, runnerID, fixture.organizationID, labelsJSON, digest); err != nil {
		t.Fatal(err)
	}
	return runnerID
}

func insertRoutingJob(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture actionsFixture,
	target string,
	labels []string,
) string {
	t.Helper()
	runID := uuid.NewString()
	jobID := uuid.NewString()
	workflowID := uuid.NewString()
	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		t.Fatal(err)
	}
	var runNumber int64
	if err := pool.QueryRow(context.Background(), `
		UPDATE repository_counters
		SET next_ci_run_number = next_ci_run_number + 1
		WHERE repository_id = $1
		RETURNING next_ci_run_number - 1
	`, fixture.repositoryID).Scan(&runNumber); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO ci_workflows (
			id, repository_id, path, name, enabled, state, last_seen_revision, trigger_config
		) VALUES ($1, $2, $3, 'Routing', true, 'active', 'revision', '{}')
	`, workflowID, fixture.repositoryID, ".github/workflows/"+workflowID+".yml"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO ci_runs (
			id, repository_id, workflow_id, run_number, run_attempt, event_name,
			branch, revision, status, event_payload
		) VALUES ($1, $2, $3, $4, 1, 'push', 'main', 'revision', 'queued', '{}')
	`, runID, fixture.repositoryID, workflowID, runNumber); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO ci_jobs (id, run_id, name, status, runner_labels, execution_target)
		VALUES ($1, $2, 'Routing', 'queued', $3, $4)
	`, jobID, runID, labelsJSON, target); err != nil {
		t.Fatal(err)
	}
	return jobID
}
