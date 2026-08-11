package collab

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const engagementMutationAttempts = 3

type RepositoryEngagementStore interface {
	SetRepositoryStar(
		context.Context, platform.User, string, bool,
	) (RepositoryEngagement, error)
	SetRepositoryWatch(
		context.Context, platform.User, string, bool,
	) (RepositoryEngagement, error)
}

func (s *store) SetRepositoryStar(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	enabled bool,
) (RepositoryEngagement, error) {
	return s.setRepositoryEngagement(ctx, actor, repositoryID, "star", enabled)
}

func (s *store) SetRepositoryWatch(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	enabled bool,
) (RepositoryEngagement, error) {
	return s.setRepositoryEngagement(ctx, actor, repositoryID, "watch", enabled)
}

func (s *store) setRepositoryEngagement(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	kind string,
	enabled bool,
) (RepositoryEngagement, error) {
	var snapshot RepositoryEngagement
	var err error
	for attempt := 0; attempt < engagementMutationAttempts; attempt++ {
		snapshot, err = s.setRepositoryEngagementOnce(ctx, actor, repositoryID, kind, enabled)
		if !isSerializationError(err) {
			return snapshot, err
		}
	}
	return RepositoryEngagement{}, fmt.Errorf("update repository %s after retries: %w", kind, err)
}

func (s *store) setRepositoryEngagementOnce(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	kind string,
	enabled bool,
) (RepositoryEngagement, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return RepositoryEngagement{}, fmt.Errorf("begin repository %s: %w", kind, err)
	}
	defer rollback(ctx, tx)
	organizationID, err := authorizeRepositoryEngagement(ctx, tx, actor.ID, repositoryID)
	if err != nil {
		return RepositoryEngagement{}, err
	}
	changed, err := mutateRepositoryEngagement(ctx, tx, actor.ID, repositoryID, kind, enabled)
	if err != nil {
		return RepositoryEngagement{}, err
	}
	snapshot, err := repositoryEngagementSnapshot(ctx, tx, actor.ID, repositoryID)
	if err != nil {
		return RepositoryEngagement{}, err
	}
	if changed {
		action, topic := engagementEvent(kind, enabled)
		if err := insertAudit(
			ctx, tx, actor.ID, organizationID, repositoryID, action, "repository", repositoryID,
		); err != nil {
			return RepositoryEngagement{}, err
		}
		payload := map[string]any{
			"repositoryId": repositoryID,
			"userId":       actor.ID,
			"enabled":      enabled,
		}
		if err := insertOutbox(
			ctx, tx, topic, repositoryID+":"+actor.ID+":"+uuidArg(), payload,
		); err != nil {
			return RepositoryEngagement{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return RepositoryEngagement{}, fmt.Errorf("commit repository %s: %w", kind, err)
	}
	return snapshot, nil
}

func authorizeRepositoryEngagement(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repositoryID string,
) (string, error) {
	var organizationID string
	err := tx.QueryRow(ctx, `
		SELECT repository.organization_id
		FROM repositories repository
		JOIN organizations organization
		  ON organization.id = repository.organization_id AND organization.active
		JOIN users actor_user ON actor_user.id = $2 AND actor_user.status = 'active'
		WHERE repository.id = $1
		  AND repository.archived_at IS NULL
		  AND repository.lifecycle_state = 'active'
		  AND (
		      repository.visibility = 'public'
		      OR repository.visibility = 'internal' AND EXISTS (
		          SELECT 1 FROM organization_memberships membership
		          WHERE membership.organization_id = organization.id
		            AND membership.user_id = $2 AND membership.active
		      )
		      OR EXISTS (
		          SELECT 1 FROM organization_memberships membership
		          WHERE membership.organization_id = organization.id
		            AND membership.user_id = $2 AND membership.active AND membership.role = 'owner'
		      )
		      OR EXISTS (
		          SELECT 1 FROM repository_memberships membership
		          WHERE membership.repository_id = repository.id
		            AND membership.user_id = $2 AND membership.active
		      )
		      OR EXISTS (
		          SELECT 1
		          FROM team_repository_roles team_role
		          JOIN teams team
		            ON team.id = team_role.team_id
		           AND team.organization_id = organization.id AND team.active
		          JOIN team_memberships team_member
		            ON team_member.team_id = team.id AND team_member.user_id = $2 AND team_member.active
		          JOIN organization_memberships organization_member
		            ON organization_member.organization_id = organization.id
		           AND organization_member.user_id = $2 AND organization_member.active
		          WHERE team_role.repository_id = repository.id AND team_role.active
		      )
		  )
		FOR SHARE OF repository, organization, actor_user
	`, repositoryID, actorID).Scan(&organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", platform.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("authorize repository engagement: %w", err)
	}
	return organizationID, nil
}

func mutateRepositoryEngagement(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repositoryID string,
	kind string,
	enabled bool,
) (bool, error) {
	var query string
	operation := "remove"
	if enabled {
		operation = "add"
	}
	switch {
	case kind == "star" && enabled:
		query = `INSERT INTO repository_stars (repository_id, user_id)
			VALUES ($1, $2) ON CONFLICT DO NOTHING`
	case kind == "star":
		query = `DELETE FROM repository_stars WHERE repository_id = $1 AND user_id = $2`
	case kind == "watch" && enabled:
		query = `INSERT INTO repository_watches (repository_id, user_id)
			VALUES ($1, $2) ON CONFLICT DO NOTHING`
	case kind == "watch":
		query = `DELETE FROM repository_watches WHERE repository_id = $1 AND user_id = $2`
	default:
		return false, errors.New("repository engagement kind is invalid")
	}
	tag, err := tx.Exec(ctx, query, repositoryID, actorID)
	if err != nil {
		return false, fmt.Errorf("%s repository %s: %w", operation, kind, err)
	}
	return tag.RowsAffected() > 0, nil
}

func repositoryEngagementSnapshot(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repositoryID string,
) (RepositoryEngagement, error) {
	var snapshot RepositoryEngagement
	err := tx.QueryRow(ctx, `
		SELECT
		  (
		      SELECT COUNT(*) FROM repository_stars star
		      JOIN users stargazer ON stargazer.id = star.user_id AND stargazer.status = 'active'
		      WHERE star.repository_id = $1
		  ),
		  (
		      SELECT COUNT(*) FROM repository_watches watch
		      JOIN users watcher ON watcher.id = watch.user_id AND watcher.status = 'active'
		      WHERE watch.repository_id = $1
		  ),
		  EXISTS (
		      SELECT 1 FROM repository_stars WHERE repository_id = $1 AND user_id = $2
		  ),
		  EXISTS (
		      SELECT 1 FROM repository_watches WHERE repository_id = $1 AND user_id = $2
		  )
	`, repositoryID, actorID).Scan(
		&snapshot.StarCount, &snapshot.WatcherCount,
		&snapshot.ViewerHasStarred, &snapshot.ViewerIsWatching,
	)
	if err != nil {
		return RepositoryEngagement{}, fmt.Errorf("read repository engagement: %w", err)
	}
	return snapshot, nil
}

func engagementEvent(kind string, enabled bool) (string, string) {
	if kind == "star" && enabled {
		return "repository.star", "repository_engagement.starred"
	}
	if kind == "star" {
		return "repository.unstar", "repository_engagement.unstarred"
	}
	if enabled {
		return "repository.watch", "repository_engagement.watched"
	}
	return "repository.unwatch", "repository_engagement.unwatched"
}

func isSerializationError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40001"
}
