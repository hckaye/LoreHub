package collab

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const maxMergeRequestAssignees = 10

var ErrMergeRequestAssigneeLimit = errors.New("a pull request can have at most 10 assignees")

type MergeRequestMetadataStore interface {
	GetMergeRequestMetadata(context.Context, string, int64) (MergeRequestMetadata, error)
	ApplyMergeRequestLabel(
		context.Context, platform.User, string, int64, string,
	) (Label, bool, error)
	RemoveMergeRequestLabel(context.Context, platform.User, string, int64, string) error
	AssignMergeRequestUser(
		context.Context, platform.User, string, int64, string,
	) (Assignee, bool, error)
	RemoveMergeRequestUser(context.Context, platform.User, string, int64, string) error
	SetMergeRequestMilestone(
		context.Context, platform.User, string, int64, *int64,
	) (*MilestoneSummary, bool, error)
}

func (s *store) GetMergeRequestMetadata(
	ctx context.Context,
	repositoryID string,
	number int64,
) (MergeRequestMetadata, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return MergeRequestMetadata{}, fmt.Errorf("begin pull request metadata read: %w", err)
	}
	defer rollback(ctx, tx)
	var mergeRequestID string
	if err := tx.QueryRow(ctx, `
		SELECT id FROM merge_requests WHERE repository_id = $1 AND number = $2
	`, repositoryID, number).Scan(&mergeRequestID); errors.Is(err, pgx.ErrNoRows) {
		return MergeRequestMetadata{}, platform.ErrNotFound
	} else if err != nil {
		return MergeRequestMetadata{}, fmt.Errorf("find pull request metadata: %w", err)
	}
	metadata := MergeRequestMetadata{
		Labels: make([]Label, 0), Assignees: make([]Assignee, 0),
	}
	if err := readMergeRequestLabels(ctx, tx, mergeRequestID, &metadata); err != nil {
		return MergeRequestMetadata{}, err
	}
	if err := readMergeRequestAssignees(ctx, tx, mergeRequestID, &metadata); err != nil {
		return MergeRequestMetadata{}, err
	}
	if err := readMergeRequestMilestone(ctx, tx, mergeRequestID, &metadata); err != nil {
		return MergeRequestMetadata{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MergeRequestMetadata{}, fmt.Errorf("commit pull request metadata read: %w", err)
	}
	return metadata, nil
}

func readMergeRequestLabels(
	ctx context.Context,
	tx pgx.Tx,
	mergeRequestID string,
	metadata *MergeRequestMetadata,
) error {
	rows, err := tx.Query(ctx, `
		SELECT label.id, label.repository_id, label.name,
		       label.description, label.color, label.created_at
		FROM merge_request_labels assignment
		JOIN labels label ON label.id = assignment.label_id
		WHERE assignment.merge_request_id = $1
		ORDER BY lower(label.name), label.id
	`, mergeRequestID)
	if err != nil {
		return fmt.Errorf("list pull request labels: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var label Label
		if err := rows.Scan(
			&label.ID, &label.RepositoryID, &label.Name,
			&label.Description, &label.Color, &label.CreatedAt,
		); err != nil {
			return fmt.Errorf("scan pull request label: %w", err)
		}
		metadata.Labels = append(metadata.Labels, label)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate pull request labels: %w", err)
	}
	return nil
}

func readMergeRequestAssignees(
	ctx context.Context,
	tx pgx.Tx,
	mergeRequestID string,
	metadata *MergeRequestMetadata,
) error {
	rows, err := tx.Query(ctx, `
		SELECT account.id, account.username, account.display_name, account.avatar_url
		FROM merge_request_assignees assignment
		JOIN users account ON account.id = assignment.user_id
		WHERE assignment.merge_request_id = $1
		ORDER BY lower(account.username), account.id
	`, mergeRequestID)
	if err != nil {
		return fmt.Errorf("list pull request assignees: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var assignee Assignee
		if err := rows.Scan(
			&assignee.ID, &assignee.Username, &assignee.DisplayName, &assignee.AvatarURL,
		); err != nil {
			return fmt.Errorf("scan pull request assignee: %w", err)
		}
		metadata.Assignees = append(metadata.Assignees, assignee)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate pull request assignees: %w", err)
	}
	return nil
}

func readMergeRequestMilestone(
	ctx context.Context,
	tx pgx.Tx,
	mergeRequestID string,
	metadata *MergeRequestMetadata,
) error {
	var milestone MilestoneSummary
	err := tx.QueryRow(ctx, `
		SELECT milestone.id, milestone.number, milestone.title,
		       milestone.state, milestone.due_on::text
		FROM merge_requests request
		JOIN repository_milestones milestone ON milestone.id = request.milestone_id
		WHERE request.id = $1
	`, mergeRequestID).Scan(
		&milestone.ID, &milestone.Number, &milestone.Title, &milestone.State, &milestone.DueOn,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pull request milestone: %w", err)
	}
	metadata.Milestone = &milestone
	return nil
}

func (s *store) ApplyMergeRequestLabel(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	labelID string,
) (Label, bool, error) {
	tx, request, err := s.beginMergeRequestMetadataMutation(ctx, actor, repositoryID, number)
	if err != nil {
		return Label{}, false, err
	}
	defer rollback(ctx, tx)
	label, err := findMergeRequestLabel(ctx, tx, repositoryID, labelID)
	if err != nil {
		return Label{}, false, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM merge_request_labels
			WHERE merge_request_id = $1 AND label_id = $2
		)
	`, request.ID, label.ID).Scan(&exists); err != nil {
		return Label{}, false, fmt.Errorf("check pull request label: %w", err)
	}
	if exists {
		if err := tx.Commit(ctx); err != nil {
			return Label{}, false, fmt.Errorf("commit existing pull request label: %w", err)
		}
		return label, false, nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO merge_request_labels (
			merge_request_id, repository_id, label_id, applied_by, applied_at
		) VALUES ($1, $2, $3, $4, $5)
	`, request.ID, repositoryID, label.ID, actor.ID, nowUTC()); err != nil {
		return Label{}, false, translateConstraintError("apply pull request label", err)
	}
	if err := finishMergeRequestMetadataMutation(
		ctx, tx, actor.ID, request, "merge_request.label.add", "label_added", label,
	); err != nil {
		return Label{}, false, err
	}
	return label, true, nil
}

func (s *store) RemoveMergeRequestLabel(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	labelID string,
) error {
	tx, request, err := s.beginMergeRequestMetadataMutation(ctx, actor, repositoryID, number)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	var label Label
	err = tx.QueryRow(ctx, `
		SELECT label.id, label.repository_id, label.name,
		       label.description, label.color, label.created_at
		FROM merge_request_labels assignment
		JOIN labels label ON label.id = assignment.label_id
		WHERE assignment.merge_request_id = $1
		  AND assignment.repository_id = $2 AND assignment.label_id = $3
	`, request.ID, repositoryID, labelID).Scan(
		&label.ID, &label.RepositoryID, &label.Name,
		&label.Description, &label.Color, &label.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return commitMissingMetadata(ctx, tx, "pull request label")
	}
	if err != nil {
		return fmt.Errorf("find pull request label assignment: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM merge_request_labels
		WHERE merge_request_id = $1 AND repository_id = $2 AND label_id = $3
	`, request.ID, repositoryID, label.ID); err != nil {
		return fmt.Errorf("remove pull request label: %w", err)
	}
	return finishMergeRequestMetadataMutation(
		ctx, tx, actor.ID, request, "merge_request.label.remove", "label_removed", label,
	)
}

func (s *store) AssignMergeRequestUser(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	username string,
) (Assignee, bool, error) {
	tx, request, err := s.beginMergeRequestMetadataMutation(ctx, actor, repositoryID, number)
	if err != nil {
		return Assignee{}, false, err
	}
	defer rollback(ctx, tx)
	assignee, err := assignableUser(ctx, tx, repositoryID, username)
	if err != nil {
		return Assignee{}, false, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM merge_request_assignees
			WHERE merge_request_id = $1 AND user_id = $2
		)
	`, request.ID, assignee.ID).Scan(&exists); err != nil {
		return Assignee{}, false, fmt.Errorf("check pull request assignee: %w", err)
	}
	if exists {
		if err := tx.Commit(ctx); err != nil {
			return Assignee{}, false, fmt.Errorf("commit existing pull request assignee: %w", err)
		}
		return assignee, false, nil
	}
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM merge_request_assignees WHERE merge_request_id = $1
	`, request.ID).Scan(&count); err != nil {
		return Assignee{}, false, fmt.Errorf("count pull request assignees: %w", err)
	}
	if count >= maxMergeRequestAssignees {
		return Assignee{}, false, ErrMergeRequestAssigneeLimit
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO merge_request_assignees (
			merge_request_id, repository_id, user_id, assigned_by, assigned_at
		) VALUES ($1, $2, $3, $4, $5)
	`, request.ID, repositoryID, assignee.ID, actor.ID, nowUTC()); err != nil {
		return Assignee{}, false, translateConstraintError("assign pull request user", err)
	}
	if err := finishMergeRequestMetadataMutation(
		ctx, tx, actor.ID, request, "merge_request.assignee.add", "assignee_added", assignee,
	); err != nil {
		return Assignee{}, false, err
	}
	return assignee, true, nil
}

func (s *store) RemoveMergeRequestUser(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	username string,
) error {
	tx, request, err := s.beginMergeRequestMetadataMutation(ctx, actor, repositoryID, number)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	var assignee Assignee
	err = tx.QueryRow(ctx, `
		SELECT account.id, account.username, account.display_name, account.avatar_url
		FROM merge_request_assignees assignment
		JOIN users account ON account.id = assignment.user_id
		WHERE assignment.merge_request_id = $1 AND assignment.repository_id = $2
		  AND lower(account.username) = lower($3)
	`, request.ID, repositoryID, username).Scan(
		&assignee.ID, &assignee.Username, &assignee.DisplayName, &assignee.AvatarURL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return commitMissingMetadata(ctx, tx, "pull request assignee")
	}
	if err != nil {
		return fmt.Errorf("find pull request assignee: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM merge_request_assignees
		WHERE merge_request_id = $1 AND repository_id = $2 AND user_id = $3
	`, request.ID, repositoryID, assignee.ID); err != nil {
		return fmt.Errorf("remove pull request assignee: %w", err)
	}
	return finishMergeRequestMetadataMutation(
		ctx, tx, actor.ID, request,
		"merge_request.assignee.remove", "assignee_removed", assignee,
	)
}

func (s *store) SetMergeRequestMilestone(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	milestoneNumber *int64,
) (*MilestoneSummary, bool, error) {
	tx, request, err := s.beginMergeRequestMetadataMutation(ctx, actor, repositoryID, number)
	if err != nil {
		return nil, false, err
	}
	defer rollback(ctx, tx)
	var milestone *MilestoneSummary
	var milestoneID any
	if milestoneNumber != nil {
		value, err := findMergeRequestMilestone(ctx, tx, repositoryID, *milestoneNumber)
		if err != nil {
			return nil, false, err
		}
		milestone = &value
		milestoneID = value.ID
	}
	var currentID *string
	if err := tx.QueryRow(ctx, `
		SELECT milestone_id FROM merge_requests WHERE id = $1 FOR UPDATE
	`, request.ID).Scan(&currentID); err != nil {
		return nil, false, fmt.Errorf("read pull request milestone assignment: %w", err)
	}
	if sameMilestone(currentID, milestone) {
		if err := tx.Commit(ctx); err != nil {
			return nil, false, fmt.Errorf("commit existing pull request milestone: %w", err)
		}
		return milestone, false, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE merge_requests SET milestone_id = $1 WHERE id = $2 AND repository_id = $3
	`, milestoneID, request.ID, repositoryID); err != nil {
		return nil, false, translateConstraintError("set pull request milestone", err)
	}
	action := "merge_request.milestone.set"
	change := "milestone_set"
	if milestone == nil {
		action = "merge_request.milestone.remove"
		change = "milestone_removed"
	}
	if err := finishMergeRequestMetadataMutation(
		ctx, tx, actor.ID, request, action, change, milestone,
	); err != nil {
		return nil, false, err
	}
	return milestone, true, nil
}

type mergeRequestMetadataRef struct {
	ID             string
	OrganizationID string
	RepositoryID   string
	Number         int64
}

func (s *store) beginMergeRequestMetadataMutation(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
) (pgx.Tx, mergeRequestMetadataRef, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, mergeRequestMetadataRef{}, fmt.Errorf("begin pull request metadata mutation: %w", err)
	}
	request, allowed, err := lockMergeRequestForMetadata(ctx, tx, actor.ID, repositoryID, number)
	if err != nil {
		rollback(ctx, tx)
		return nil, mergeRequestMetadataRef{}, err
	}
	if !allowed {
		rollback(ctx, tx)
		return nil, mergeRequestMetadataRef{}, platform.ErrForbidden
	}
	return tx, request, nil
}

func lockMergeRequestForMetadata(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repositoryID string,
	number int64,
) (mergeRequestMetadataRef, bool, error) {
	var request mergeRequestMetadataRef
	var allowed bool
	err := tx.QueryRow(ctx, `
		SELECT merge_request.id, repository.organization_id, merge_request.number,
		       EXISTS (
		         SELECT 1 FROM users actor
		         WHERE actor.id = $3 AND actor.status = 'active' AND (
		           EXISTS (
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
		FROM merge_requests merge_request
		JOIN repositories repository
		  ON repository.id = merge_request.repository_id
		 AND repository.lifecycle_state = 'active' AND repository.archived_at IS NULL
		JOIN organizations organization
		  ON organization.id = repository.organization_id AND organization.active
		WHERE merge_request.repository_id = $1 AND merge_request.number = $2
		FOR UPDATE OF merge_request
	`, repositoryID, number, actorID).Scan(
		&request.ID, &request.OrganizationID, &request.Number, &allowed,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return mergeRequestMetadataRef{}, false, platform.ErrNotFound
	}
	if err != nil {
		return mergeRequestMetadataRef{}, false, fmt.Errorf("lock pull request metadata: %w", err)
	}
	request.RepositoryID = repositoryID
	return request, allowed, nil
}

func findMergeRequestLabel(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	labelID string,
) (Label, error) {
	var label Label
	err := tx.QueryRow(ctx, `
		SELECT id, repository_id, name, description, color, created_at
		FROM labels WHERE id = $1 AND repository_id = $2
	`, labelID, repositoryID).Scan(
		&label.ID, &label.RepositoryID, &label.Name,
		&label.Description, &label.Color, &label.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Label{}, platform.ErrNotFound
	}
	if err != nil {
		return Label{}, fmt.Errorf("find pull request label: %w", err)
	}
	return label, nil
}

func findMergeRequestMilestone(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	number int64,
) (MilestoneSummary, error) {
	var milestone MilestoneSummary
	err := tx.QueryRow(ctx, `
		SELECT id, number, title, state, due_on::text
		FROM repository_milestones WHERE repository_id = $1 AND number = $2
	`, repositoryID, number).Scan(
		&milestone.ID, &milestone.Number, &milestone.Title, &milestone.State, &milestone.DueOn,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return MilestoneSummary{}, platform.ErrNotFound
	}
	if err != nil {
		return MilestoneSummary{}, fmt.Errorf("find pull request milestone: %w", err)
	}
	return milestone, nil
}

func sameMilestone(currentID *string, milestone *MilestoneSummary) bool {
	if currentID == nil || milestone == nil {
		return currentID == nil && milestone == nil
	}
	return *currentID == milestone.ID
}

func commitMissingMetadata(ctx context.Context, tx pgx.Tx, kind string) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit missing %s: %w", kind, err)
	}
	return nil
}

func finishMergeRequestMetadataMutation(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	request mergeRequestMetadataRef,
	action string,
	change string,
	subject any,
) error {
	if _, err := tx.Exec(ctx, `
		UPDATE merge_requests SET updated_at = $1
		WHERE id = $2 AND repository_id = $3
	`, nowUTC(), request.ID, request.RepositoryID); err != nil {
		return fmt.Errorf("update pull request metadata timestamp: %w", err)
	}
	if err := insertAudit(
		ctx, tx, actorID, request.OrganizationID, request.RepositoryID,
		action, "merge_request", request.ID,
	); err != nil {
		return err
	}
	payload := map[string]any{
		"mergeRequestId": request.ID,
		"repositoryId":   request.RepositoryID,
		"number":         request.Number,
		"change":         change,
		"subject":        subject,
	}
	if err := insertOutbox(
		ctx, tx, "merge_request.updated", request.ID+":"+uuidArg(), payload,
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit pull request metadata mutation: %w", err)
	}
	return nil
}
