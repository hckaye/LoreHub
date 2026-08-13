package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func workflowSchedulingJobs(
	ctx context.Context,
	transaction pgx.Tx,
	workflowID string,
	workflowRevisionID *uuid.UUID,
	workflowName string,
	legacyEnvironment string,
) ([]WorkflowJobDefinition, error) {
	var triggerConfig json.RawMessage
	var err error
	if workflowRevisionID != nil {
		err = transaction.QueryRow(ctx, `
			SELECT trigger_config FROM ci_workflow_revisions WHERE id = $1
		`, *workflowRevisionID).Scan(&triggerConfig)
	} else {
		err = transaction.QueryRow(ctx, `
			SELECT trigger_config FROM ci_workflows WHERE id = NULLIF($1, '')::uuid
		`, workflowID).Scan(&triggerConfig)
	}
	if err != nil {
		return nil, fmt.Errorf("read workflow scheduling configuration: %w", err)
	}
	var config struct {
		Environment  string                  `json:"environment"`
		RunnerLabels []string                `json:"runner_labels"`
		Jobs         []WorkflowJobDefinition `json:"jobs"`
	}
	if err := json.Unmarshal(triggerConfig, &config); err != nil {
		return nil, fmt.Errorf("decode workflow scheduling configuration: %w", err)
	}
	if len(config.Jobs) == 0 {
		labels, err := normalizedStoredRunnerLabels(config.RunnerLabels)
		if err != nil {
			return nil, err
		}
		if len(labels) == 0 {
			labels = []string{"ubuntu-latest"}
		}
		if legacyEnvironment == "" {
			legacyEnvironment = config.Environment
		}
		return []WorkflowJobDefinition{{
			JobName: "", RunnerLabels: labels, Needs: []string{}, Environment: legacyEnvironment,
		}}, nil
	}
	jobNames := make(map[string]struct{}, len(config.Jobs))
	for index := range config.Jobs {
		job := &config.Jobs[index]
		if job.JobName == "" || len(job.JobName) > 255 || strings.ContainsAny(job.JobName, "\x00\r\n") {
			return nil, errors.New("stored workflow job name is invalid")
		}
		if _, duplicate := jobNames[job.JobName]; duplicate {
			return nil, fmt.Errorf("stored workflow job %q is duplicated", job.JobName)
		}
		jobNames[job.JobName] = struct{}{}
		job.RunnerLabels, err = normalizedStoredRunnerLabels(job.RunnerLabels)
		if err != nil {
			return nil, fmt.Errorf("normalize stored workflow job %q runner labels: %w", job.JobName, err)
		}
		if len(job.RunnerLabels) == 0 {
			return nil, fmt.Errorf("stored workflow job %q has no runner labels", job.JobName)
		}
		if job.Needs == nil {
			job.Needs = []string{}
		}
	}
	if err := validateWorkflowJobDependencies(config.Jobs, jobNames); err != nil {
		return nil, fmt.Errorf("validate stored workflow dependencies: %w", err)
	}
	return config.Jobs, nil
}

func normalizedStoredRunnerLabels(labels []string) ([]string, error) {
	result := make([]string, 0, len(labels))
	seen := make(map[string]struct{}, len(labels))
	for _, value := range labels {
		label := strings.ToLower(strings.TrimSpace(value))
		if !runnerLabelPattern.MatchString(label) {
			return nil, fmt.Errorf("stored workflow runner label %q is invalid", value)
		}
		if _, duplicate := seen[label]; duplicate {
			continue
		}
		seen[label] = struct{}{}
		result = append(result, label)
	}
	sort.Strings(result)
	return result, nil
}

func executionTargetForLabels(labels []string) string {
	if containsRunnerLabel(labels, "self-hosted") {
		return "self_hosted"
	}
	return "managed"
}

func hasManagedSchedulingJob(jobs []WorkflowJobDefinition) bool {
	for _, job := range jobs {
		if executionTargetForLabels(job.RunnerLabels) == "managed" {
			return true
		}
	}
	return false
}

func skipJobsWithFailedDependencies(ctx context.Context, transaction pgx.Tx, runID string) error {
	_, err := transaction.Exec(ctx, `
		WITH RECURSIVE blocked(job_name) AS (
			SELECT dependent.job_name
			FROM ci_jobs dependent
			WHERE dependent.run_id = $1
			  AND dependent.status = 'queued'
			  AND EXISTS (
			    SELECT 1
			    FROM jsonb_array_elements_text(dependent.needs) required(job_name)
			    JOIN ci_jobs dependency
			      ON dependency.run_id = dependent.run_id
			     AND dependency.job_name = required.job_name
			    WHERE dependency.status IN ('completed', 'cancelled')
			      AND dependency.conclusion IS DISTINCT FROM 'success'
			  )
			UNION
			SELECT dependent.job_name
			FROM ci_jobs dependent
			JOIN blocked dependency ON dependent.needs ? dependency.job_name
			WHERE dependent.run_id = $1 AND dependent.status = 'queued'
		), skipped AS (
			UPDATE ci_jobs job
			SET status = 'completed', conclusion = 'skipped', completed_at = now(),
			    lease_owner = NULL, lease_expires_at = NULL
			WHERE job.run_id = $1 AND job.status = 'queued'
			  AND job.job_name IN (SELECT job_name FROM blocked)
			RETURNING job.id
		)
		UPDATE deployments
		SET status = 'cancelled', completed_at = now(), updated_at = now()
		WHERE job_id IN (SELECT id FROM skipped)
		  AND status IN ('pending', 'waiting', 'queued')
	`, runID)
	if err != nil {
		return fmt.Errorf("skip CI jobs with failed dependencies: %w", err)
	}
	return nil
}

func aggregateRunJobs(
	ctx context.Context,
	transaction pgx.Tx,
	runID string,
) (bool, string, error) {
	var activeJobs, failedJobs, timedOutJobs, cancelledJobs, successfulJobs int64
	err := transaction.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE status IN ('queued', 'in_progress')),
		       COUNT(*) FILTER (WHERE conclusion = 'failure'),
		       COUNT(*) FILTER (WHERE conclusion = 'timed_out'),
		       COUNT(*) FILTER (WHERE conclusion = 'cancelled'),
		       COUNT(*) FILTER (WHERE conclusion = 'success')
		FROM ci_jobs WHERE run_id = $1
	`, runID).Scan(&activeJobs, &failedJobs, &timedOutJobs, &cancelledJobs, &successfulJobs)
	if err != nil {
		return false, "", fmt.Errorf("aggregate CI run jobs: %w", err)
	}
	if activeJobs > 0 {
		return false, "", nil
	}
	conclusion := "skipped"
	switch {
	case failedJobs > 0:
		conclusion = "failure"
	case timedOutJobs > 0:
		conclusion = "timed_out"
	case cancelledJobs > 0:
		conclusion = "cancelled"
	case successfulJobs > 0:
		conclusion = "success"
	}
	command, err := transaction.Exec(ctx, `
		UPDATE ci_runs
		SET status = 'completed', conclusion = $2, completed_at = now()
		WHERE id = $1 AND status NOT IN ('completed', 'cancelled')
	`, runID, conclusion)
	if err != nil {
		return false, "", fmt.Errorf("complete CI run: %w", err)
	}
	return command.RowsAffected() == 1, conclusion, nil
}
