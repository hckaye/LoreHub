package collab

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type MergeRequestDraftStore interface {
	SetMergeRequestDraft(
		context.Context, platform.User, string, int64, bool,
	) (MergeRequest, bool, error)
}

func (s *store) SetMergeRequestDraft(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	isDraft bool,
) (MergeRequest, bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return MergeRequest{}, false, fmt.Errorf("begin pull request draft update: %w", err)
	}
	defer rollback(ctx, tx)
	ref, err := lockMergeRequestDraft(ctx, tx, actor.ID, repositoryID, number)
	if err != nil {
		return MergeRequest{}, false, err
	}
	if !ref.Allowed {
		return MergeRequest{}, false, platform.ErrForbidden
	}
	if ref.IsDraft == isDraft {
		mergeRequest, err := scanMergeRequestByTx(ctx, tx, repositoryID, number)
		if err != nil {
			return MergeRequest{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return MergeRequest{}, false, fmt.Errorf("commit unchanged pull request draft: %w", err)
		}
		return mergeRequest, false, nil
	}
	if ref.State != "open" {
		return MergeRequest{}, false, platform.ErrConflict
	}
	if isDraft {
		busy, err := mergePushInProgress(ctx, tx, ref.ID)
		if err != nil {
			return MergeRequest{}, false, err
		}
		if busy {
			return MergeRequest{}, false, ErrMergeBusy
		}
	}
	now := nowUTC()
	if _, err := tx.Exec(ctx, `
		UPDATE merge_requests
		SET is_draft = $1, draft_changed_at = $2, draft_changed_by = $3, updated_at = $2
		WHERE id = $4 AND repository_id = $5 AND state = 'open'
	`, isDraft, now, actor.ID, ref.ID, repositoryID); err != nil {
		return MergeRequest{}, false, translateConstraintError("update pull request draft", err)
	}
	mergeRequest, err := scanMergeRequestByTx(ctx, tx, repositoryID, number)
	if err != nil {
		return MergeRequest{}, false, err
	}
	action := "merge_request.mark_ready"
	change := "marked_ready"
	if isDraft {
		action = "merge_request.convert_to_draft"
		change = "converted_to_draft"
	}
	if err := insertAudit(
		ctx, tx, actor.ID, ref.OrganizationID, repositoryID,
		action, "merge_request", ref.ID,
	); err != nil {
		return MergeRequest{}, false, err
	}
	payload := map[string]any{
		"mergeRequestId": ref.ID,
		"repositoryId":   repositoryID,
		"number":         number,
		"change":         change,
		"isDraft":        isDraft,
	}
	if err := insertOutbox(ctx, tx, "merge_request.updated", ref.ID+":"+uuidArg(), payload); err != nil {
		return MergeRequest{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MergeRequest{}, false, fmt.Errorf("commit pull request draft update: %w", err)
	}
	return mergeRequest, true, nil
}

type mergeRequestDraftRef struct {
	ID             string
	OrganizationID string
	State          string
	IsDraft        bool
	Allowed        bool
}

func lockMergeRequestDraft(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repositoryID string,
	number int64,
) (mergeRequestDraftRef, error) {
	var ref mergeRequestDraftRef
	err := tx.QueryRow(ctx, `
		SELECT request.id, repository.organization_id, request.state, request.is_draft,
		       EXISTS (
		         SELECT 1 FROM users actor
		         WHERE actor.id = $3 AND actor.status = 'active' AND (
		           (
		             request.author_id = actor.id AND (
		               repository.visibility = 'public'
		               OR EXISTS (
		                 SELECT 1 FROM organization_memberships membership
		                 WHERE membership.organization_id = repository.organization_id
		                   AND membership.user_id = actor.id AND membership.active
		                   AND (repository.visibility = 'internal' OR membership.role = 'owner')
		               )
		               OR EXISTS (
		                 SELECT 1 FROM repository_memberships membership
		                 WHERE membership.repository_id = repository.id
		                   AND membership.user_id = actor.id AND membership.active
		               )
		               OR EXISTS (
		                 SELECT 1 FROM team_repository_roles role
		                 JOIN teams team ON team.id = role.team_id
		                   AND team.organization_id = repository.organization_id AND team.active
		                 JOIN team_memberships member
		                   ON member.team_id = team.id AND member.user_id = actor.id AND member.active
		                 JOIN organization_memberships organization_member
		                   ON organization_member.organization_id = repository.organization_id
		                   AND organization_member.user_id = actor.id AND organization_member.active
		                 WHERE role.repository_id = repository.id AND role.active
		               )
		             )
		           )
		           OR EXISTS (
		             SELECT 1 FROM organization_memberships membership
		             WHERE membership.organization_id = repository.organization_id
		               AND membership.user_id = actor.id AND membership.active
		               AND membership.role = 'owner'
		           )
		           OR EXISTS (
		             SELECT 1 FROM repository_memberships membership
		             WHERE membership.repository_id = repository.id
		               AND membership.user_id = actor.id AND membership.active
		               AND membership.role IN ('triage', 'write', 'maintain', 'admin')
		           )
		           OR EXISTS (
		             SELECT 1 FROM team_repository_roles role
		             JOIN teams team ON team.id = role.team_id
		               AND team.organization_id = repository.organization_id AND team.active
		             JOIN team_memberships member
		               ON member.team_id = team.id AND member.user_id = actor.id AND member.active
		             JOIN organization_memberships organization_member
		               ON organization_member.organization_id = repository.organization_id
		               AND organization_member.user_id = actor.id AND organization_member.active
		             WHERE role.repository_id = repository.id AND role.active
		               AND role.role IN ('triage', 'write', 'maintain', 'admin')
		           )
		         )
		       )
		FROM merge_requests request
		JOIN repositories repository ON repository.id = request.repository_id
		 AND repository.lifecycle_state = 'active' AND repository.archived_at IS NULL
		JOIN organizations organization ON organization.id = repository.organization_id
		 AND organization.active
		WHERE request.repository_id = $1 AND request.number = $2
		FOR UPDATE OF request
	`, repositoryID, number, actorID).Scan(
		&ref.ID, &ref.OrganizationID, &ref.State, &ref.IsDraft, &ref.Allowed,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return mergeRequestDraftRef{}, platform.ErrNotFound
	}
	if err != nil {
		return mergeRequestDraftRef{}, fmt.Errorf("lock pull request draft: %w", err)
	}
	return ref, nil
}

func mergePushInProgress(ctx context.Context, tx pgx.Tx, mergeRequestID string) (bool, error) {
	var busy bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM merge_operations
			WHERE merge_request_id = $1 AND state IN ('pushing', 'pushed', 'merged')
		)
	`, mergeRequestID).Scan(&busy); err != nil {
		return false, fmt.Errorf("check pull request merge progress: %w", err)
	}
	return busy, nil
}
