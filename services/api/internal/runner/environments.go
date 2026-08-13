package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxEnvironmentReviewers = 6

type EnvironmentReviewer struct {
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

type EnvironmentRecord struct {
	ID                string                `json:"id"`
	Name              string                `json:"name"`
	WaitTimerMinutes  int                   `json:"waitTimerMinutes"`
	PreventSelfReview bool                  `json:"preventSelfReview"`
	Reviewers         []EnvironmentReviewer `json:"reviewers"`
	CreatedAt         time.Time             `json:"createdAt"`
	UpdatedAt         time.Time             `json:"updatedAt"`
}

type EnvironmentInput struct {
	WaitTimerMinutes  int
	PreventSelfReview bool
	ReviewerUsernames []string
}

type DeploymentRecord struct {
	ID               string     `json:"id"`
	EnvironmentName  string     `json:"environmentName"`
	RunNumber        int64      `json:"runNumber"`
	WorkflowName     string     `json:"workflowName"`
	Branch           string     `json:"branch"`
	Revision         string     `json:"revision"`
	Status           string     `json:"status"`
	ActorUsername    string     `json:"actorUsername,omitempty"`
	ReviewedUsername string     `json:"reviewedUsername,omitempty"`
	CanReview        bool       `json:"canReview"`
	WaitUntil        time.Time  `json:"waitUntil"`
	ReviewedAt       *time.Time `json:"reviewedAt"`
	StartedAt        *time.Time `json:"startedAt"`
	CompletedAt      *time.Time `json:"completedAt"`
	CreatedAt        time.Time  `json:"createdAt"`
}

func (store *Store) enqueueDeployment(
	ctx context.Context,
	tx pgx.Tx,
	repository Repository,
	runID uuid.UUID,
	jobID uuid.UUID,
	environmentName string,
	actorID string,
	branch string,
	revision string,
) error {
	if !validActionsEnvironmentName(environmentName) {
		return fmt.Errorf("%w: workflow environment is invalid", ErrActionInvalid)
	}
	environmentID := uuid.New()
	var waitTimerMinutes int
	err := tx.QueryRow(ctx, `
		INSERT INTO repository_environments (id, repository_id, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (repository_id, lower(name)) DO UPDATE SET
			name = EXCLUDED.name,
			active = true,
			updated_at = now()
		RETURNING id, wait_timer_minutes
	`, environmentID, repository.ID, environmentName).Scan(&environmentID, &waitTimerMinutes)
	if err != nil {
		return fmt.Errorf("resolve deployment environment: %w", err)
	}
	var reviewerCount int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM repository_environment_reviewers assignment
		JOIN users reviewer ON reviewer.id = assignment.user_id AND reviewer.status = 'active'
		JOIN repositories repository ON repository.id = $2
		JOIN organization_memberships membership
		  ON membership.organization_id = repository.organization_id
		 AND membership.user_id = reviewer.id
		 AND membership.active
		WHERE assignment.environment_id = $1
	`, environmentID, repository.ID).Scan(&reviewerCount); err != nil {
		return fmt.Errorf("count deployment reviewers: %w", err)
	}
	status := "queued"
	if reviewerCount > 0 {
		status = "pending"
	} else if waitTimerMinutes > 0 {
		status = "waiting"
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO deployments (
			id, repository_id, environment_id, environment_name, run_id, job_id,
			actor_id, branch, revision, status, wait_until
		) VALUES (
			$1, $2, $3, $4, $5, $6, NULLIF($7, '')::uuid, $8, $9, $10,
			now() + make_interval(mins => $11)
		)
	`, uuid.New(), repository.ID, environmentID, environmentName, runID, jobID,
		actorID, branch, revision, status, waitTimerMinutes)
	if err != nil {
		return fmt.Errorf("enqueue deployment: %w", err)
	}
	return nil
}

func (store *Store) ListEnvironments(
	ctx context.Context,
	access RepositoryAccess,
	actorID string,
) ([]EnvironmentRecord, error) {
	actor, err := uuid.Parse(actorID)
	if err != nil {
		return nil, ErrActionForbidden
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin environment list: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := authorizeEnvironmentAdmin(ctx, tx, access, actor); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT environment.id::text, environment.name, environment.wait_timer_minutes,
		       environment.prevent_self_review, environment.created_at, environment.updated_at,
		       COALESCE(jsonb_agg(jsonb_build_object(
		           'userId', reviewer.id::text,
		           'username', reviewer.username,
		           'displayName', reviewer.display_name
		       ) ORDER BY lower(reviewer.username)) FILTER (WHERE reviewer.id IS NOT NULL), '[]'::jsonb)
		FROM repository_environments environment
		LEFT JOIN repository_environment_reviewers assignment
		  ON assignment.environment_id = environment.id
		LEFT JOIN users reviewer ON reviewer.id = assignment.user_id AND reviewer.status = 'active'
		WHERE environment.repository_id = $1 AND environment.active
		GROUP BY environment.id
		ORDER BY lower(environment.name)
	`, access.ID)
	if err != nil {
		return nil, fmt.Errorf("list environments: %w", err)
	}
	environments := make([]EnvironmentRecord, 0)
	for rows.Next() {
		var environment EnvironmentRecord
		var reviewers []byte
		if err := rows.Scan(
			&environment.ID,
			&environment.Name,
			&environment.WaitTimerMinutes,
			&environment.PreventSelfReview,
			&environment.CreatedAt,
			&environment.UpdatedAt,
			&reviewers,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan environment: %w", err)
		}
		if err := json.Unmarshal(reviewers, &environment.Reviewers); err != nil {
			rows.Close()
			return nil, fmt.Errorf("decode environment reviewers: %w", err)
		}
		environments = append(environments, environment)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate environments: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit environment list: %w", err)
	}
	return environments, nil
}

func (store *Store) UpsertEnvironment(
	ctx context.Context,
	access RepositoryAccess,
	actorID string,
	name string,
	input EnvironmentInput,
) (EnvironmentRecord, error) {
	actor, err := uuid.Parse(actorID)
	if err != nil {
		return EnvironmentRecord{}, ErrActionForbidden
	}
	name, usernames, err := validateEnvironmentInput(name, input)
	if err != nil {
		return EnvironmentRecord{}, err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return EnvironmentRecord{}, fmt.Errorf("begin environment update: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := authorizeEnvironmentAdmin(ctx, tx, access, actor); err != nil {
		return EnvironmentRecord{}, err
	}
	reviewers, err := resolveEnvironmentReviewers(ctx, tx, access, usernames)
	if err != nil {
		return EnvironmentRecord{}, err
	}
	environmentID := uuid.New()
	err = tx.QueryRow(ctx, `
		INSERT INTO repository_environments (
			id, repository_id, name, wait_timer_minutes, prevent_self_review, created_by
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (repository_id, lower(name)) DO UPDATE SET
			name = EXCLUDED.name,
			wait_timer_minutes = EXCLUDED.wait_timer_minutes,
			prevent_self_review = EXCLUDED.prevent_self_review,
			active = true,
			updated_at = now()
		RETURNING id
	`, environmentID, access.ID, name, input.WaitTimerMinutes, input.PreventSelfReview, actor).
		Scan(&environmentID)
	if err != nil {
		return EnvironmentRecord{}, fmt.Errorf("save environment: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM repository_environment_reviewers WHERE environment_id = $1
	`, environmentID); err != nil {
		return EnvironmentRecord{}, fmt.Errorf("replace environment reviewers: %w", err)
	}
	for _, reviewer := range reviewers {
		if _, err := tx.Exec(ctx, `
			INSERT INTO repository_environment_reviewers (environment_id, user_id) VALUES ($1, $2)
		`, environmentID, reviewer.UserID); err != nil {
			return EnvironmentRecord{}, fmt.Errorf("save environment reviewer: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE deployments
		SET wait_until = created_at + make_interval(mins => $2),
		    status = CASE
		      WHEN $3 > 0 THEN 'pending'
		      WHEN created_at + make_interval(mins => $2) <= now() THEN 'queued'
		      ELSE 'waiting'
		    END,
		    reviewed_by = NULL,
		    reviewed_at = NULL,
		    updated_at = now()
		WHERE environment_id = $1 AND status IN ('pending', 'waiting', 'queued')
	`, environmentID, input.WaitTimerMinutes, len(reviewers)); err != nil {
		return EnvironmentRecord{}, fmt.Errorf("refresh pending environment deployments: %w", err)
	}
	if err := recordEnvironmentEvent(
		ctx,
		tx,
		access,
		actorID,
		"actions.environment.updated",
		environmentID.String(),
		map[string]any{"name": name, "reviewerCount": len(reviewers)},
	); err != nil {
		return EnvironmentRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return EnvironmentRecord{}, fmt.Errorf("commit environment update: %w", err)
	}
	return store.environmentByID(ctx, environmentID.String())
}

func (store *Store) DeleteEnvironment(
	ctx context.Context,
	access RepositoryAccess,
	actorID string,
	name string,
) error {
	actor, err := uuid.Parse(actorID)
	if err != nil {
		return ErrActionForbidden
	}
	if !validActionsEnvironmentName(name) {
		return ErrActionInvalid
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin environment deletion: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := authorizeEnvironmentAdmin(ctx, tx, access, actor); err != nil {
		return err
	}
	var environmentID string
	err = tx.QueryRow(ctx, `
		SELECT id FROM repository_environments
		WHERE repository_id = $1 AND lower(name) = lower($2) AND active
		FOR UPDATE
	`, access.ID, name).Scan(&environmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrActionNotFound
	}
	if err != nil {
		return fmt.Errorf("find environment to delete: %w", err)
	}
	var activeDeployments bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM deployments
			WHERE environment_id = $1 AND status IN ('pending', 'waiting', 'queued', 'in_progress')
		)
	`, environmentID).Scan(&activeDeployments); err != nil {
		return fmt.Errorf("check active environment deployments: %w", err)
	}
	if activeDeployments {
		return ErrActionConflict
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM repository_environment_reviewers WHERE environment_id = $1
	`, environmentID); err != nil {
		return fmt.Errorf("remove environment reviewers: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE repository_environments
		SET active = false, wait_timer_minutes = 0, prevent_self_review = true, updated_at = now()
		WHERE id = $1
	`, environmentID); err != nil {
		return fmt.Errorf("delete environment: %w", err)
	}
	if err := recordEnvironmentEvent(
		ctx,
		tx,
		access,
		actorID,
		"actions.environment.deleted",
		environmentID,
		map[string]any{"name": name},
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit environment deletion: %w", err)
	}
	return nil
}

func (store *Store) ListDeployments(
	ctx context.Context,
	access RepositoryAccess,
	actorID string,
	limit int,
) ([]DeploymentRecord, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := store.pool.Query(ctx, `
		SELECT deployment.id::text, deployment.environment_name, run.run_number,
		       COALESCE(workflow.name, workflow_revision.name, ''), deployment.branch,
		       deployment.revision, deployment.status, COALESCE(actor.username, ''),
		       COALESCE(reviewer.username, ''), deployment.wait_until, deployment.reviewed_at,
		       deployment.started_at, deployment.completed_at, deployment.created_at,
		       $2 <> '' AND deployment.status = 'pending'
		       AND EXISTS (
		           SELECT 1
		           FROM repository_environment_reviewers assignment
		           JOIN users candidate ON candidate.id = assignment.user_id AND candidate.status = 'active'
		           JOIN organization_memberships membership
		             ON membership.organization_id = $3
		            AND membership.user_id = candidate.id
		            AND membership.active
		           WHERE assignment.environment_id = deployment.environment_id
		             AND candidate.id = NULLIF($2, '')::uuid
		             AND (
		               repository.visibility IN ('public', 'internal')
		               OR membership.role = 'owner'
		               OR EXISTS (
		                 SELECT 1 FROM repository_memberships direct_access
		                 WHERE direct_access.repository_id = repository.id
		                   AND direct_access.user_id = candidate.id AND direct_access.active
		               )
		               OR EXISTS (
		                 SELECT 1
		                 FROM team_repository_roles role
		                 JOIN teams team ON team.id = role.team_id AND team.active
		                 JOIN team_memberships team_member
		                   ON team_member.team_id = team.id
		                  AND team_member.user_id = candidate.id
		                  AND team_member.active
		                 WHERE role.repository_id = repository.id AND role.active
		               )
		             )
		       )
		       AND (
		           NOT environment.prevent_self_review
		           OR run.actor_id IS NULL
		           OR run.actor_id <> NULLIF($2, '')::uuid
		       )
		FROM deployments deployment
		JOIN repository_environments environment ON environment.id = deployment.environment_id
		JOIN repositories repository
		  ON repository.id = deployment.repository_id AND repository.organization_id = $3
		JOIN ci_runs run ON run.id = deployment.run_id
		LEFT JOIN ci_workflows workflow ON workflow.id = run.workflow_id
		LEFT JOIN ci_workflow_revisions workflow_revision
		  ON workflow_revision.id = run.workflow_revision_id
		LEFT JOIN users actor ON actor.id = deployment.actor_id
		LEFT JOIN users reviewer ON reviewer.id = deployment.reviewed_by
		WHERE deployment.repository_id = $1
		ORDER BY deployment.created_at DESC, deployment.id DESC
		LIMIT $4
	`, access.ID, actorID, access.OrganizationID, limit)
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	defer rows.Close()
	deployments := make([]DeploymentRecord, 0)
	for rows.Next() {
		var deployment DeploymentRecord
		if err := rows.Scan(
			&deployment.ID,
			&deployment.EnvironmentName,
			&deployment.RunNumber,
			&deployment.WorkflowName,
			&deployment.Branch,
			&deployment.Revision,
			&deployment.Status,
			&deployment.ActorUsername,
			&deployment.ReviewedUsername,
			&deployment.WaitUntil,
			&deployment.ReviewedAt,
			&deployment.StartedAt,
			&deployment.CompletedAt,
			&deployment.CreatedAt,
			&deployment.CanReview,
		); err != nil {
			return nil, fmt.Errorf("scan deployment: %w", err)
		}
		deployments = append(deployments, deployment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deployments: %w", err)
	}
	return deployments, nil
}

func (store *Store) ReviewDeployment(
	ctx context.Context,
	access RepositoryAccess,
	actorID string,
	deploymentID string,
	approved bool,
) (DeploymentRecord, error) {
	if access.Archived {
		return DeploymentRecord{}, ErrActionForbidden
	}
	actor, err := uuid.Parse(actorID)
	if err != nil {
		return DeploymentRecord{}, ErrActionForbidden
	}
	deploymentUUID, err := uuid.Parse(deploymentID)
	if err != nil {
		return DeploymentRecord{}, ErrActionNotFound
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return DeploymentRecord{}, fmt.Errorf("begin deployment review: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var status, runID, jobID, environmentID string
	var runActor *uuid.UUID
	var preventSelfReview bool
	err = tx.QueryRow(ctx, `
		SELECT deployment.status, deployment.run_id::text, deployment.job_id::text,
		       deployment.environment_id::text, run.actor_id, environment.prevent_self_review
		FROM deployments deployment
		JOIN ci_runs run ON run.id = deployment.run_id
		JOIN repository_environments environment ON environment.id = deployment.environment_id
		WHERE deployment.id = $1 AND deployment.repository_id = $2 AND environment.active
		FOR UPDATE OF deployment, run
	`, deploymentUUID, access.ID).Scan(
		&status,
		&runID,
		&jobID,
		&environmentID,
		&runActor,
		&preventSelfReview,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeploymentRecord{}, ErrActionNotFound
	}
	if err != nil {
		return DeploymentRecord{}, fmt.Errorf("find deployment to review: %w", err)
	}
	if status != "pending" {
		return DeploymentRecord{}, ErrActionConflict
	}
	if preventSelfReview && runActor != nil && *runActor == actor {
		return DeploymentRecord{}, ErrActionForbidden
	}
	var eligible bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM repository_environment_reviewers assignment
			JOIN users reviewer ON reviewer.id = assignment.user_id AND reviewer.status = 'active'
			JOIN repositories repository
			  ON repository.id = $4 AND repository.organization_id = $3
			JOIN organization_memberships membership
			  ON membership.organization_id = $3
			 AND membership.user_id = reviewer.id
			 AND membership.active
			WHERE assignment.environment_id = $1 AND reviewer.id = $2
			  AND (
			    repository.visibility IN ('public', 'internal')
			    OR membership.role = 'owner'
			    OR EXISTS (
			      SELECT 1 FROM repository_memberships direct_access
			      WHERE direct_access.repository_id = repository.id
			        AND direct_access.user_id = reviewer.id AND direct_access.active
			    )
			    OR EXISTS (
			      SELECT 1
			      FROM team_repository_roles role
			      JOIN teams team ON team.id = role.team_id AND team.active
			      JOIN team_memberships team_member
			        ON team_member.team_id = team.id
			       AND team_member.user_id = reviewer.id
			       AND team_member.active
			      WHERE role.repository_id = repository.id AND role.active
			    )
			  )
		)
	`, environmentID, actor, access.OrganizationID, access.ID).Scan(&eligible); err != nil {
		return DeploymentRecord{}, fmt.Errorf("authorize deployment review: %w", err)
	}
	if !eligible {
		return DeploymentRecord{}, ErrActionForbidden
	}
	action := "approved"
	if approved {
		if _, err := tx.Exec(ctx, `
			UPDATE deployments
			SET status = CASE WHEN wait_until <= now() THEN 'queued' ELSE 'waiting' END,
			    reviewed_by = $2, reviewed_at = now(), updated_at = now()
			WHERE id = $1
		`, deploymentUUID, actor); err != nil {
			return DeploymentRecord{}, fmt.Errorf("approve deployment: %w", err)
		}
	} else {
		action = "rejected"
		if _, err := tx.Exec(ctx, `
			UPDATE deployments
			SET status = 'rejected', reviewed_by = $2, reviewed_at = now(),
			    completed_at = now(), updated_at = now()
			WHERE id = $1
		`, deploymentUUID, actor); err != nil {
			return DeploymentRecord{}, fmt.Errorf("reject deployment: %w", err)
		}
		jobCommand, err := tx.Exec(ctx, `
			UPDATE ci_jobs
			SET status = 'completed', conclusion = 'failure', completed_at = now()
			WHERE id = $1 AND status = 'queued'
		`, jobID)
		if err != nil {
			return DeploymentRecord{}, fmt.Errorf("reject deployment job: %w", err)
		}
		if jobCommand.RowsAffected() != 1 {
			return DeploymentRecord{}, ErrActionConflict
		}
		if err := skipJobsWithFailedDependencies(ctx, tx, runID); err != nil {
			return DeploymentRecord{}, err
		}
		completed, conclusion, err := aggregateRunJobs(ctx, tx, runID)
		if err != nil {
			return DeploymentRecord{}, err
		}
		if completed {
			if err := recordCompletionEvents(ctx, tx, runID, conclusion); err != nil {
				return DeploymentRecord{}, err
			}
		}
	}
	if err := recordEnvironmentEvent(
		ctx,
		tx,
		access,
		actorID,
		"actions.deployment."+action,
		deploymentID,
		map[string]any{"runId": runID},
	); err != nil {
		return DeploymentRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeploymentRecord{}, fmt.Errorf("commit deployment review: %w", err)
	}
	return store.deploymentByID(ctx, access, actorID, deploymentID)
}

func validateEnvironmentInput(name string, input EnvironmentInput) (string, []string, error) {
	if !validActionsEnvironmentName(name) || input.WaitTimerMinutes < 0 || input.WaitTimerMinutes > 43200 ||
		len(input.ReviewerUsernames) > maxEnvironmentReviewers {
		return "", nil, ErrActionInvalid
	}
	usernames := make([]string, 0, len(input.ReviewerUsernames))
	seen := make(map[string]struct{}, len(input.ReviewerUsernames))
	for _, username := range input.ReviewerUsernames {
		username = strings.TrimSpace(username)
		key := strings.ToLower(username)
		if username == "" || len(username) > 64 || strings.ContainsAny(username, "\x00\r\n") {
			return "", nil, ErrActionInvalid
		}
		if _, duplicate := seen[key]; duplicate {
			return "", nil, ErrActionInvalid
		}
		seen[key] = struct{}{}
		usernames = append(usernames, key)
	}
	return name, usernames, nil
}

func validActionsEnvironmentName(name string) bool {
	if name == "" || len(name) > 128 || strings.TrimSpace(name) != name || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func resolveEnvironmentReviewers(
	ctx context.Context,
	tx pgx.Tx,
	access RepositoryAccess,
	usernames []string,
) ([]EnvironmentReviewer, error) {
	if len(usernames) == 0 {
		return []EnvironmentReviewer{}, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT reviewer.id::text, reviewer.username, reviewer.display_name
		FROM users reviewer
		JOIN repositories repository ON repository.id = $1 AND repository.organization_id = $2
		JOIN organization_memberships membership
		  ON membership.organization_id = repository.organization_id
		 AND membership.user_id = reviewer.id
		 AND membership.active
		WHERE reviewer.status = 'active' AND lower(reviewer.username) = ANY($3::text[])
		  AND (
		    repository.visibility IN ('public', 'internal')
		    OR membership.role = 'owner'
		    OR EXISTS (
		      SELECT 1 FROM repository_memberships direct_access
		      WHERE direct_access.repository_id = repository.id
		        AND direct_access.user_id = reviewer.id AND direct_access.active
		    )
		    OR EXISTS (
		      SELECT 1
		      FROM team_repository_roles role
		      JOIN teams team ON team.id = role.team_id AND team.active
		      JOIN team_memberships team_member
		        ON team_member.team_id = team.id
		       AND team_member.user_id = reviewer.id
		       AND team_member.active
		      WHERE role.repository_id = repository.id AND role.active
		    )
		  )
		ORDER BY lower(reviewer.username)
	`, access.ID, access.OrganizationID, usernames)
	if err != nil {
		return nil, fmt.Errorf("resolve environment reviewers: %w", err)
	}
	defer rows.Close()
	reviewers := make([]EnvironmentReviewer, 0, len(usernames))
	for rows.Next() {
		var reviewer EnvironmentReviewer
		if err := rows.Scan(&reviewer.UserID, &reviewer.Username, &reviewer.DisplayName); err != nil {
			return nil, fmt.Errorf("scan environment reviewer: %w", err)
		}
		reviewers = append(reviewers, reviewer)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate environment reviewers: %w", err)
	}
	if len(reviewers) != len(usernames) {
		return nil, ErrActionInvalid
	}
	return reviewers, nil
}

func authorizeEnvironmentAdmin(
	ctx context.Context,
	tx pgx.Tx,
	access RepositoryAccess,
	actorID uuid.UUID,
) error {
	var allowed bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM repositories repository
			JOIN organizations organization
			  ON organization.id = repository.organization_id AND organization.active
			JOIN users actor ON actor.id = $3 AND actor.status = 'active'
			WHERE repository.id = $1 AND repository.organization_id = $2
			  AND repository.lifecycle_state = 'active' AND repository.archived_at IS NULL
			  AND (
			    EXISTS (
			      SELECT 1 FROM organization_memberships membership
			      WHERE membership.organization_id = organization.id
			        AND membership.user_id = actor.id AND membership.active AND membership.role = 'owner'
			    )
			    OR EXISTS (
			      SELECT 1 FROM repository_memberships membership
			      WHERE membership.repository_id = repository.id
			        AND membership.user_id = actor.id AND membership.active AND membership.role = 'admin'
			    )
			    OR EXISTS (
			      SELECT 1
			      FROM team_repository_roles role
			      JOIN teams team ON team.id = role.team_id AND team.active
			      JOIN team_memberships member
			        ON member.team_id = team.id AND member.user_id = actor.id AND member.active
			      JOIN organization_memberships organization_member
			        ON organization_member.organization_id = organization.id
			       AND organization_member.user_id = actor.id AND organization_member.active
			      WHERE role.repository_id = repository.id AND role.active AND role.role = 'admin'
			    )
			  )
		)
	`, access.ID, access.OrganizationID, actorID).Scan(&allowed)
	if err != nil {
		return fmt.Errorf("authorize environment administration: %w", err)
	}
	if !allowed {
		return ErrActionForbidden
	}
	return nil
}

func (store *Store) environmentByID(ctx context.Context, environmentID string) (EnvironmentRecord, error) {
	var environment EnvironmentRecord
	var reviewers []byte
	err := store.pool.QueryRow(ctx, `
		SELECT environment.id::text, environment.name, environment.wait_timer_minutes,
		       environment.prevent_self_review, environment.created_at, environment.updated_at,
		       COALESCE(jsonb_agg(jsonb_build_object(
		           'userId', reviewer.id::text,
		           'username', reviewer.username,
		           'displayName', reviewer.display_name
		       ) ORDER BY lower(reviewer.username)) FILTER (WHERE reviewer.id IS NOT NULL), '[]'::jsonb)
		FROM repository_environments environment
		LEFT JOIN repository_environment_reviewers assignment
		  ON assignment.environment_id = environment.id
		LEFT JOIN users reviewer ON reviewer.id = assignment.user_id AND reviewer.status = 'active'
		WHERE environment.id = $1 AND environment.active
		GROUP BY environment.id
	`, environmentID).Scan(
		&environment.ID,
		&environment.Name,
		&environment.WaitTimerMinutes,
		&environment.PreventSelfReview,
		&environment.CreatedAt,
		&environment.UpdatedAt,
		&reviewers,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return EnvironmentRecord{}, ErrActionNotFound
	}
	if err != nil {
		return EnvironmentRecord{}, fmt.Errorf("read environment: %w", err)
	}
	if err := json.Unmarshal(reviewers, &environment.Reviewers); err != nil {
		return EnvironmentRecord{}, fmt.Errorf("decode environment reviewers: %w", err)
	}
	return environment, nil
}

func (store *Store) deploymentByID(
	ctx context.Context,
	access RepositoryAccess,
	actorID string,
	deploymentID string,
) (DeploymentRecord, error) {
	deployments, err := store.ListDeployments(ctx, access, actorID, 100)
	if err != nil {
		return DeploymentRecord{}, err
	}
	for _, deployment := range deployments {
		if deployment.ID == deploymentID {
			return deployment, nil
		}
	}
	return DeploymentRecord{}, ErrActionNotFound
}

func recordEnvironmentEvent(
	ctx context.Context,
	tx pgx.Tx,
	access RepositoryAccess,
	actorID string,
	action string,
	targetID string,
	details map[string]any,
) error {
	encoded, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode environment audit event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			id, organization_id, repository_id, actor_id, action, target_type, target_id, details
		) VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, 'actions_environment', $6, $7)
	`, uuid.New(), access.OrganizationID, access.ID, actorID, action, targetID, encoded); err != nil {
		return fmt.Errorf("record environment audit event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, $2, $3, $4)
	`, uuid.New(), action, targetID+":"+uuid.NewString(), encoded); err != nil {
		return fmt.Errorf("record environment outbox event: %w", err)
	}
	return nil
}
