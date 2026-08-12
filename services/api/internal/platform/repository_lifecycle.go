package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (store *Store) SetRepositoryArchived(
	ctx context.Context,
	actor User,
	owner string,
	slug string,
	archived bool,
	confirmation string,
) (Repository, error) {
	if strings.TrimSpace(confirmation) != owner+"/"+slug {
		return Repository{}, ErrInvalidInput
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Repository{}, fmt.Errorf("begin repository archive update: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	var repositoryID, organizationID string
	var archivedAt *time.Time
	err = transaction.QueryRow(ctx, repositoryArchiveManagerQuery, owner, slug, actor.ID).Scan(
		&repositoryID, &organizationID, &archivedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Repository{}, store.repositoryArchiveAccessError(ctx, transaction, actor.ID, owner, slug)
	}
	if err != nil {
		return Repository{}, fmt.Errorf("authorize repository archive update: %w", err)
	}
	if (archivedAt != nil) == archived {
		return commitRepositoryArchiveSnapshot(ctx, transaction, repositoryID)
	}
	if err := updateRepositoryArchive(ctx, transaction, actor.ID, repositoryID, archived); err != nil {
		return Repository{}, err
	}
	if archived {
		if err := cancelRepositoryRuns(ctx, transaction, repositoryID); err != nil {
			return Repository{}, err
		}
	}
	action := "repository.unarchive"
	topic := "repository.unarchived"
	if archived {
		action = "repository.archive"
		topic = "repository.archived"
	}
	if err := insertAudit(ctx, transaction, actor.ID, organizationID, repositoryID, action,
		"repository", repositoryID); err != nil {
		return Repository{}, err
	}
	if err := insertOutbox(ctx, transaction, topic, repositoryID+":"+uuid.NewString(), map[string]any{
		"repositoryId": repositoryID,
		"owner":        owner,
		"repository":   slug,
		"archived":     archived,
	}); err != nil {
		return Repository{}, err
	}
	return commitRepositoryArchiveSnapshot(ctx, transaction, repositoryID)
}

const repositoryArchiveManagerQuery = `
	SELECT repository.id, repository.organization_id, repository.archived_at
	FROM repositories repository
	JOIN organizations organization
	  ON organization.id = repository.organization_id AND organization.active
	JOIN users actor_user ON actor_user.id = $3 AND actor_user.status = 'active'
	WHERE organization.slug = $1 AND repository.slug = $2
	  AND repository.lifecycle_state = 'active'
	  AND (
	      EXISTS (
	          SELECT 1 FROM organization_memberships membership
	          WHERE membership.organization_id = organization.id
	            AND membership.user_id = $3 AND membership.role = 'owner' AND membership.active
	      )
	      OR EXISTS (
	          SELECT 1 FROM repository_memberships membership
	          WHERE membership.repository_id = repository.id
	            AND membership.user_id = $3 AND membership.role = 'admin' AND membership.active
	      )
	      OR EXISTS (
	          SELECT 1
	          FROM team_repository_roles role
	          JOIN teams team
	            ON team.id = role.team_id AND team.organization_id = organization.id AND team.active
	          JOIN team_memberships team_member
	            ON team_member.team_id = team.id AND team_member.user_id = $3 AND team_member.active
	          JOIN organization_memberships organization_member
	            ON organization_member.organization_id = organization.id
	           AND organization_member.user_id = $3 AND organization_member.active
	          WHERE role.repository_id = repository.id AND role.role = 'admin' AND role.active
	      )
	  )
	FOR UPDATE OF repository
`

func (store *Store) repositoryArchiveAccessError(
	ctx context.Context,
	transaction pgx.Tx,
	actorID string,
	owner string,
	slug string,
) error {
	var visible bool
	err := transaction.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM repositories repository
			JOIN organizations organization ON organization.id = repository.organization_id
			WHERE organization.slug = $1 AND repository.slug = $2
			  AND `+repositoryAccessClause("repository", "$3")+`
		)
	`, owner, slug, actorID).Scan(&visible)
	if err != nil {
		return fmt.Errorf("check repository archive visibility: %w", err)
	}
	if visible {
		return ErrForbidden
	}
	return ErrNotFound
}

func updateRepositoryArchive(
	ctx context.Context,
	transaction pgx.Tx,
	actorID string,
	repositoryID string,
	archived bool,
) error {
	if archived {
		_, err := transaction.Exec(ctx, `
			UPDATE repositories
			SET archived_at = now(), archived_by = $2, updated_at = now()
			WHERE id = $1
		`, repositoryID, actorID)
		if err != nil {
			return fmt.Errorf("archive repository: %w", err)
		}
		return nil
	}
	_, err := transaction.Exec(ctx, `
		UPDATE repositories
		SET archived_at = NULL, archived_by = NULL, updated_at = now()
		WHERE id = $1
	`, repositoryID)
	if err != nil {
		return fmt.Errorf("unarchive repository: %w", err)
	}
	return nil
}

func cancelRepositoryRuns(ctx context.Context, transaction pgx.Tx, repositoryID string) error {
	_, err := transaction.Exec(ctx, `
		UPDATE deployments
		SET status = 'cancelled', completed_at = COALESCE(completed_at, now()), updated_at = now()
		WHERE repository_id = $1 AND status IN ('pending', 'waiting', 'queued', 'in_progress')
	`, repositoryID)
	if err != nil {
		return fmt.Errorf("cancel repository deployments: %w", err)
	}
	_, err = transaction.Exec(ctx, `
		UPDATE ci_jobs job
		SET status = 'cancelled', conclusion = 'cancelled',
		    completed_at = COALESCE(job.completed_at, now()),
		    lease_owner = NULL, lease_expires_at = NULL
		FROM ci_runs run
		WHERE run.id = job.run_id AND run.repository_id = $1
		  AND job.status IN ('queued', 'in_progress')
	`, repositoryID)
	if err != nil {
		return fmt.Errorf("cancel repository Actions jobs: %w", err)
	}
	_, err = transaction.Exec(ctx, `
		UPDATE ci_runs
		SET status = 'cancelled', conclusion = 'cancelled', cancel_requested = true,
		    completed_at = COALESCE(completed_at, now())
		WHERE repository_id = $1 AND status IN ('queued', 'in_progress')
	`, repositoryID)
	if err != nil {
		return fmt.Errorf("cancel repository Actions runs: %w", err)
	}
	return nil
}

func commitRepositoryArchiveSnapshot(
	ctx context.Context,
	transaction pgx.Tx,
	repositoryID string,
) (Repository, error) {
	repository, err := scanRepository(transaction.QueryRow(ctx, repositorySelect+`
		WHERE r.id = $1
		GROUP BY r.id, o.slug
	`, repositoryID))
	if err != nil {
		return Repository{}, fmt.Errorf("read repository archive state: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return Repository{}, fmt.Errorf("commit repository archive update: %w", err)
	}
	return repository, nil
}
