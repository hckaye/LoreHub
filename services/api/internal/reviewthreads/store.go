package reviewthreads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const (
	permissionRead  = 1
	permissionWrite = 3
)

type store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) Store {
	return &store{pool: pool}
}

type lockedRequest struct {
	id             string
	authorID       string
	state          string
	baseRevision   string
	headRevision   string
	permission     int
	organizationID string
}

func (store *store) begin(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	operation string,
) (pgx.Tx, lockedRequest, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, lockedRequest{}, fmt.Errorf("begin %s: %w", operation, err)
	}
	request, err := lockRequest(ctx, tx, actor.ID, repository, number)
	if err != nil {
		rollback(ctx, tx)
		return nil, lockedRequest{}, err
	}
	return tx, request, nil
}

func lockRequest(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repository RepositoryRef,
	number int64,
) (lockedRequest, error) {
	var result lockedRequest
	err := tx.QueryRow(ctx, `
		SELECT request.id, request.author_id, request.state,
		       request.target_revision, request.source_revision, repository.organization_id,
		       GREATEST(
		         CASE
		           WHEN repository.visibility = 'public' THEN 1
		           WHEN repository.visibility = 'internal' AND EXISTS (
		             SELECT 1 FROM organization_memberships membership
		             WHERE membership.organization_id = organization.id
		               AND membership.user_id = actor.id AND membership.active
		           ) THEN 1 ELSE 0
		         END,
		         COALESCE((
		           SELECT CASE membership.role
		             WHEN 'owner' THEN 4
		             WHEN 'maintainer' THEN CASE WHEN repository.visibility <> 'private' THEN 1 ELSE 0 END
		             WHEN 'member' THEN CASE WHEN repository.visibility <> 'private' THEN 1 ELSE 0 END
		             ELSE 0 END
		           FROM organization_memberships membership
		           WHERE membership.organization_id = organization.id
		             AND membership.user_id = actor.id AND membership.active
		         ), 0),
		         COALESCE((
		           SELECT CASE membership.role
		             WHEN 'admin' THEN 4 WHEN 'maintain' THEN 3 WHEN 'write' THEN 3
		             WHEN 'triage' THEN 2 WHEN 'read' THEN 1 ELSE 0 END
		           FROM repository_memberships membership
		           WHERE membership.repository_id = repository.id
		             AND membership.user_id = actor.id AND membership.active
		         ), 0),
		         COALESCE((
		           SELECT MAX(CASE role.role
		             WHEN 'admin' THEN 4 WHEN 'maintain' THEN 3 WHEN 'write' THEN 3
		             WHEN 'triage' THEN 2 WHEN 'read' THEN 1 ELSE 0 END)
		           FROM team_repository_roles role
		           JOIN teams team ON team.id = role.team_id
		             AND team.organization_id = organization.id AND team.active
		           JOIN team_memberships team_member ON team_member.team_id = team.id
		             AND team_member.user_id = actor.id AND team_member.active
		           JOIN organization_memberships organization_member
		             ON organization_member.organization_id = organization.id
		             AND organization_member.user_id = actor.id AND organization_member.active
		           WHERE role.repository_id = repository.id AND role.active
		         ), 0)
		       )
		FROM merge_requests request
		JOIN repositories repository ON repository.id = request.repository_id
		  AND repository.organization_id = $2 AND repository.archived_at IS NULL
		  AND repository.lifecycle_state = 'active'
		JOIN organizations organization ON organization.id = repository.organization_id AND organization.active
		JOIN users actor ON actor.id = $4 AND actor.status = 'active'
		WHERE repository.id = $1 AND request.number = $3
		FOR UPDATE OF request
	`, repository.ID, repository.OrganizationID, number, actorID).Scan(
		&result.id, &result.authorID, &result.state, &result.baseRevision,
		&result.headRevision, &result.organizationID, &result.permission,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRequest{}, platform.ErrNotFound
	}
	if err != nil {
		return lockedRequest{}, fmt.Errorf("lock pull request for review: %w", err)
	}
	if result.permission < permissionRead {
		return lockedRequest{}, platform.ErrForbidden
	}
	return result, nil
}

func insertAudit(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repository RepositoryRef,
	action string,
	targetType string,
	targetID string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			id, organization_id, repository_id, actor_id, action, target_type, target_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, uuid.NewString(), repository.OrganizationID, repository.ID, actorID, action, targetType, targetID)
	if err != nil {
		return fmt.Errorf("record review thread audit event: %w", err)
	}
	return nil
}

func insertOutbox(
	ctx context.Context,
	tx pgx.Tx,
	topic string,
	repositoryID string,
	requestID string,
	threadID string,
	commentID string,
) error {
	payload, err := json.Marshal(map[string]string{
		"repositoryId":   repositoryID,
		"mergeRequestId": requestID,
		"threadId":       threadID,
		"commentId":      commentID,
	})
	if err != nil {
		return fmt.Errorf("encode review thread outbox event: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, $2, $3, $4)
	`, uuid.NewString(), topic, threadID+":"+uuid.NewString(), payload)
	if err != nil {
		return fmt.Errorf("record review thread outbox event: %w", err)
	}
	return nil
}

func touchRequest(ctx context.Context, tx pgx.Tx, requestID string) error {
	if _, err := tx.Exec(ctx, `UPDATE merge_requests SET updated_at = now() WHERE id = $1`, requestID); err != nil {
		return fmt.Errorf("update pull request timestamp: %w", err)
	}
	return nil
}

func commit(ctx context.Context, tx pgx.Tx, operation string) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s: %w", operation, err)
	}
	return nil
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(context.WithoutCancel(ctx))
}

func invalid(detail string) error {
	return fmt.Errorf("%s: %w", detail, ErrInvalidInput)
}

func translate(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return fmt.Errorf("%s: %w", operation, platform.ErrConflict)
		case "23514", "22001":
			return fmt.Errorf("%s: %w", operation, ErrInvalidInput)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func normalizeBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" || len(body) > 1<<20 {
		return "", invalid("comment body must contain between 1 and 1048576 bytes")
	}
	return body, nil
}
