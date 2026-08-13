package collab

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const maxIssueAssignees = 10

var ErrAssigneeLimit = errors.New("an issue can have at most 10 assignees")

type IssueAssigneeStore interface {
	ListAssignableUsers(
		context.Context, string, string, Page,
	) (Result[Assignee], error)
	AssignIssueUser(
		context.Context, platform.User, string, int64, string,
	) (Assignee, bool, error)
	RemoveIssueUser(
		context.Context, platform.User, string, int64, string,
	) error
}

func (s *store) ListAssignableUsers(
	ctx context.Context,
	repositoryID string,
	query string,
	page Page,
) (Result[Assignee], error) {
	offset, err := pageOffset(page)
	if err != nil {
		return Result[Assignee]{}, err
	}
	limit := page.Limit
	if limit < 1 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	rows, err := s.pool.Query(ctx, `
		SELECT account.id, account.username, account.display_name, account.avatar_url
		FROM users account
		JOIN repositories repository
		  ON repository.id = $1
		 AND repository.archived_at IS NULL
		 AND repository.lifecycle_state = 'active'
		JOIN organizations organization
		  ON organization.id = repository.organization_id
		 AND organization.active
		WHERE account.status = 'active'
		  AND ($2 = '' OR account.username ILIKE '%' || $2 || '%'
		       OR account.display_name ILIKE '%' || $2 || '%')
		  AND (
			EXISTS (
				SELECT 1 FROM organization_memberships membership
				WHERE membership.organization_id = organization.id
				  AND membership.user_id = account.id
				  AND membership.active
				  AND (
					membership.role = 'owner'
					OR repository.visibility = 'internal'
				  )
			)
			OR EXISTS (
				SELECT 1 FROM repository_memberships membership
				WHERE membership.repository_id = repository.id
				  AND membership.user_id = account.id
				  AND membership.active
			)
			OR EXISTS (
				SELECT 1
				FROM team_repository_roles role
				JOIN teams team
				  ON team.id = role.team_id
				 AND team.organization_id = organization.id
				 AND team.active
				JOIN team_memberships team_membership
				  ON team_membership.team_id = team.id
				 AND team_membership.user_id = account.id
				 AND team_membership.active
				JOIN organization_memberships organization_membership
				  ON organization_membership.organization_id = organization.id
				 AND organization_membership.user_id = account.id
				 AND organization_membership.active
				WHERE role.repository_id = repository.id AND role.active
			)
		  )
		ORDER BY lower(account.username), account.id
		LIMIT $3 OFFSET $4
	`, repositoryID, query, limit+1, offset)
	if err != nil {
		return Result[Assignee]{}, fmt.Errorf("list assignable users: %w", err)
	}
	defer rows.Close()
	assignees := make([]Assignee, 0, limit+1)
	for rows.Next() {
		var assignee Assignee
		if err := rows.Scan(
			&assignee.ID, &assignee.Username, &assignee.DisplayName, &assignee.AvatarURL,
		); err != nil {
			return Result[Assignee]{}, fmt.Errorf("scan assignable user: %w", err)
		}
		assignees = append(assignees, assignee)
	}
	if err := rows.Err(); err != nil {
		return Result[Assignee]{}, fmt.Errorf("iterate assignable users: %w", err)
	}
	return paginate(assignees, limit, offset), nil
}

func (s *store) AssignIssueUser(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	issueNumber int64,
	username string,
) (Assignee, bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Assignee{}, false, fmt.Errorf("begin issue assignee transaction: %w", err)
	}
	defer rollback(ctx, tx)
	issueID, organizationID, allowed, err := lockIssueForAssignment(
		ctx, tx, actor.ID, repositoryID, issueNumber,
	)
	if err != nil {
		return Assignee{}, false, err
	}
	if !allowed {
		return Assignee{}, false, platform.ErrForbidden
	}
	assignee, err := assignableUser(ctx, tx, repositoryID, username)
	if err != nil {
		return Assignee{}, false, err
	}
	var alreadyAssigned bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM issue_assignees
			WHERE issue_id = $1 AND user_id = $2
		)
	`, issueID, assignee.ID).Scan(&alreadyAssigned); err != nil {
		return Assignee{}, false, fmt.Errorf("check issue assignee: %w", err)
	}
	if alreadyAssigned {
		if err := tx.Commit(ctx); err != nil {
			return Assignee{}, false, fmt.Errorf("commit existing issue assignee: %w", err)
		}
		return assignee, false, nil
	}
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM issue_assignees WHERE issue_id = $1
	`, issueID).Scan(&count); err != nil {
		return Assignee{}, false, fmt.Errorf("count issue assignees: %w", err)
	}
	if count >= maxIssueAssignees {
		return Assignee{}, false, ErrAssigneeLimit
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO issue_assignees (
			issue_id, repository_id, user_id, assigned_by, assigned_at
		) VALUES ($1, $2, $3, $4, $5)
	`, issueID, repositoryID, assignee.ID, actor.ID, nowUTC()); err != nil {
		return Assignee{}, false, translateConstraintError("assign issue user", err)
	}
	if err := finishIssueAssigneeMutation(
		ctx, tx, actor.ID, organizationID, repositoryID, issueID,
		issueNumber, "issue.assignee.add", "added", assignee,
	); err != nil {
		return Assignee{}, false, err
	}
	return assignee, true, nil
}

func (s *store) RemoveIssueUser(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	issueNumber int64,
	username string,
) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin issue assignee removal: %w", err)
	}
	defer rollback(ctx, tx)
	issueID, organizationID, allowed, err := lockIssueForAssignment(
		ctx, tx, actor.ID, repositoryID, issueNumber,
	)
	if err != nil {
		return err
	}
	if !allowed {
		return platform.ErrForbidden
	}
	var assignee Assignee
	err = tx.QueryRow(ctx, `
		SELECT account.id, account.username, account.display_name, account.avatar_url
		FROM issue_assignees assignment
		JOIN users account ON account.id = assignment.user_id
		WHERE assignment.issue_id = $1 AND lower(account.username) = lower($2)
	`, issueID, username).Scan(
		&assignee.ID, &assignee.Username, &assignee.DisplayName, &assignee.AvatarURL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit missing issue assignee: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("find assigned issue user: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM issue_assignees WHERE issue_id = $1 AND user_id = $2
	`, issueID, assignee.ID); err != nil {
		return fmt.Errorf("remove issue assignee: %w", err)
	}
	return finishIssueAssigneeMutation(
		ctx, tx, actor.ID, organizationID, repositoryID, issueID,
		issueNumber, "issue.assignee.remove", "removed", assignee,
	)
}

func lockIssueForAssignment(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repositoryID string,
	issueNumber int64,
) (string, string, bool, error) {
	var issueID, organizationID string
	var allowed bool
	err := tx.QueryRow(ctx, `
		SELECT issue.id, repository.organization_id,
		       EXISTS (
				SELECT 1 FROM users actor
				WHERE actor.id = $3 AND actor.status = 'active'
				  AND (
					EXISTS (
						SELECT 1 FROM organization_memberships membership
						WHERE membership.organization_id = repository.organization_id
						  AND membership.user_id = actor.id
						  AND membership.active AND membership.role = 'owner'
					)
					OR EXISTS (
						SELECT 1 FROM repository_memberships membership
						WHERE membership.repository_id = repository.id
						  AND membership.user_id = actor.id AND membership.active
						  AND membership.role IN ('triage', 'write', 'maintain', 'admin')
					)
					OR EXISTS (
						SELECT 1
						FROM team_repository_roles role
						JOIN teams team
						  ON team.id = role.team_id
						 AND team.organization_id = repository.organization_id
						 AND team.active
						JOIN team_memberships team_membership
						  ON team_membership.team_id = team.id
						 AND team_membership.user_id = actor.id
						 AND team_membership.active
						JOIN organization_memberships organization_membership
						  ON organization_membership.organization_id = repository.organization_id
						 AND organization_membership.user_id = actor.id
						 AND organization_membership.active
						WHERE role.repository_id = repository.id AND role.active
						  AND role.role IN ('triage', 'write', 'maintain', 'admin')
					)
				  )
		       )
		FROM issues issue
		JOIN repositories repository
		  ON repository.id = issue.repository_id
		 AND repository.archived_at IS NULL
		 AND repository.lifecycle_state = 'active'
		JOIN organizations organization
		  ON organization.id = repository.organization_id
		 AND organization.active
		WHERE issue.repository_id = $1 AND issue.number = $2
		FOR UPDATE OF issue
	`, repositoryID, issueNumber, actorID).Scan(&issueID, &organizationID, &allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, platform.ErrNotFound
	}
	if err != nil {
		return "", "", false, fmt.Errorf("lock issue for assignee mutation: %w", err)
	}
	return issueID, organizationID, allowed, nil
}

func assignableUser(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	username string,
) (Assignee, error) {
	var assignee Assignee
	err := tx.QueryRow(ctx, `
		SELECT account.id, account.username, account.display_name, account.avatar_url
		FROM users account
		JOIN repositories repository
		  ON repository.id = $1
		 AND repository.archived_at IS NULL
		 AND repository.lifecycle_state = 'active'
		JOIN organizations organization
		  ON organization.id = repository.organization_id AND organization.active
		WHERE account.status = 'active' AND lower(account.username) = lower($2)
		  AND (
			EXISTS (
				SELECT 1 FROM organization_memberships membership
				WHERE membership.organization_id = organization.id
				  AND membership.user_id = account.id AND membership.active
				  AND (membership.role = 'owner' OR repository.visibility = 'internal')
			)
			OR EXISTS (
				SELECT 1 FROM repository_memberships membership
				WHERE membership.repository_id = repository.id
				  AND membership.user_id = account.id AND membership.active
			)
			OR EXISTS (
				SELECT 1
				FROM team_repository_roles role
				JOIN teams team
				  ON team.id = role.team_id
				 AND team.organization_id = organization.id AND team.active
				JOIN team_memberships team_membership
				  ON team_membership.team_id = team.id
				 AND team_membership.user_id = account.id AND team_membership.active
				JOIN organization_memberships organization_membership
				  ON organization_membership.organization_id = organization.id
				 AND organization_membership.user_id = account.id
				 AND organization_membership.active
				WHERE role.repository_id = repository.id AND role.active
			)
		  )
	`, repositoryID, username).Scan(
		&assignee.ID, &assignee.Username, &assignee.DisplayName, &assignee.AvatarURL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Assignee{}, platform.ErrNotFound
	}
	if err != nil {
		return Assignee{}, fmt.Errorf("find assignable user: %w", err)
	}
	return assignee, nil
}

func finishIssueAssigneeMutation(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	organizationID string,
	repositoryID string,
	issueID string,
	issueNumber int64,
	action string,
	change string,
	assignee Assignee,
) error {
	if _, err := tx.Exec(ctx, `
		UPDATE issues SET updated_at = $1 WHERE id = $2 AND repository_id = $3
	`, nowUTC(), issueID, repositoryID); err != nil {
		return fmt.Errorf("update issue assignee timestamp: %w", err)
	}
	if err := insertAudit(
		ctx, tx, actorID, organizationID, repositoryID, action, "issue", issueID,
	); err != nil {
		return err
	}
	payload := map[string]any{
		"issueNumber": issueNumber,
		"change":      change,
		"assignee":    assignee,
	}
	if err := insertOutbox(
		ctx, tx, "issue.assignees.updated", issueID+":"+uuidArg(), payload,
	); err != nil {
		return err
	}
	eventKind := EventAssigned
	if change == "removed" {
		eventKind = EventUnassigned
	}
	assigned := assignee
	if err := RecordWorkItemEvent(ctx, tx, WorkItemEventRecord{
		RepositoryID: repositoryID, ItemKind: WorkItemIssue, ItemID: issueID,
		ActorID: actorID, Kind: eventKind,
		Payload: WorkItemEventPayload{Assignee: &assigned},
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit issue assignee mutation: %w", err)
	}
	return nil
}
