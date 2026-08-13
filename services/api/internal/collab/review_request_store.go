package collab

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const maxReviewRequests = 15

var ErrReviewRequestLimit = errors.New("a pull request can have at most 15 review requests")

type ReviewRequestStore interface {
	ListReviewRequests(context.Context, string, int64) ([]ReviewRequest, error)
	ListReviewCandidates(
		context.Context, platform.User, string, int64, string,
	) ([]ReviewCandidate, error)
	RequestUserReview(
		context.Context, platform.User, string, int64, string,
	) (ReviewRequest, bool, error)
	RequestTeamReview(
		context.Context, platform.User, string, int64, string,
	) (ReviewRequest, bool, error)
	RemoveUserReviewRequest(context.Context, platform.User, string, int64, string) error
	RemoveTeamReviewRequest(context.Context, platform.User, string, int64, string) error
}

func (s *store) ListReviewRequests(
	ctx context.Context,
	repositoryID string,
	number int64,
) ([]ReviewRequest, error) {
	if _, err := s.GetMergeRequest(ctx, repositoryID, number); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT request.id,
		       CASE WHEN request.reviewer_user_id IS NOT NULL THEN 'user' ELSE 'team' END,
		       COALESCE(reviewer.username, team.slug),
		       COALESCE(reviewer.display_name, team.display_name),
		       COALESCE(reviewer.avatar_url, ''),
		       CASE
		         WHEN request.reviewer_user_id IS NOT NULL THEN COALESCE((
		           SELECT review.decision
		           FROM merge_request_reviews review
		           JOIN users active_reviewer
		             ON active_reviewer.id = review.reviewer_id
		            AND active_reviewer.status = 'active'
		           WHERE review.merge_request_id = merge_request.id
		             AND review.reviewer_id = request.reviewer_user_id
		             AND review.source_revision = merge_request.source_revision
		         ), 'pending')
		         ELSE COALESCE((
		           SELECT review.decision
		           FROM team_memberships membership
		           JOIN organization_memberships organization_member
		             ON organization_member.organization_id = request.organization_id
		            AND organization_member.user_id = membership.user_id
		            AND organization_member.active
		           JOIN users active_reviewer
		             ON active_reviewer.id = membership.user_id
		            AND active_reviewer.status = 'active'
		           JOIN merge_request_reviews review
		             ON review.reviewer_id = membership.user_id
		            AND review.merge_request_id = merge_request.id
		            AND review.source_revision = merge_request.source_revision
		           WHERE membership.team_id = request.reviewer_team_id AND membership.active
		           ORDER BY CASE review.decision
		             WHEN 'changes_requested' THEN 3
		             WHEN 'approved' THEN 2
		             ELSE 1
		           END DESC, review.created_at DESC
		           LIMIT 1
		         ), 'pending')
		       END,
		       requester.username, request.requested_at
		FROM merge_request_review_requests request
		JOIN merge_requests merge_request ON merge_request.id = request.merge_request_id
		LEFT JOIN users reviewer ON reviewer.id = request.reviewer_user_id
		LEFT JOIN teams team ON team.id = request.reviewer_team_id
		JOIN users requester ON requester.id = request.requested_by
		WHERE request.repository_id = $1 AND merge_request.number = $2
		  AND request.removed_at IS NULL
		ORDER BY request.requested_at, request.id
	`, repositoryID, number)
	if err != nil {
		return nil, fmt.Errorf("list review requests: %w", err)
	}
	defer rows.Close()
	requests := make([]ReviewRequest, 0)
	for rows.Next() {
		var request ReviewRequest
		if err := rows.Scan(
			&request.ID, &request.Kind, &request.Slug, &request.DisplayName,
			&request.AvatarURL, &request.Status, &request.RequestedBy, &request.RequestedAt,
		); err != nil {
			return nil, fmt.Errorf("scan review request: %w", err)
		}
		requests = append(requests, request)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review requests: %w", err)
	}
	return requests, nil
}

func (s *store) ListReviewCandidates(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	query string,
) ([]ReviewCandidate, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin review candidate transaction: %w", err)
	}
	defer rollback(ctx, tx)
	request, err := findReviewRequestContext(ctx, tx, actor.ID, repositoryID, number, false)
	if err != nil {
		return nil, err
	}
	if !request.Allowed {
		return nil, platform.ErrForbidden
	}
	candidates, err := listUserReviewCandidates(ctx, tx, request, query)
	if err != nil {
		return nil, err
	}
	teams, err := listTeamReviewCandidates(ctx, tx, request, query)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, teams...)
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit review candidate transaction: %w", err)
	}
	return candidates, nil
}

func (s *store) RequestUserReview(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	username string,
) (ReviewRequest, bool, error) {
	return s.requestReview(ctx, actor, repositoryID, number, "user", username)
}

func (s *store) RequestTeamReview(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	team string,
) (ReviewRequest, bool, error) {
	return s.requestReview(ctx, actor, repositoryID, number, "team", team)
}

func (s *store) requestReview(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	kind string,
	slug string,
) (ReviewRequest, bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ReviewRequest{}, false, fmt.Errorf("begin review request transaction: %w", err)
	}
	defer rollback(ctx, tx)
	requestContext, err := findReviewRequestContext(ctx, tx, actor.ID, repositoryID, number, true)
	if err != nil {
		return ReviewRequest{}, false, err
	}
	if !requestContext.Allowed {
		return ReviewRequest{}, false, platform.ErrForbidden
	}
	candidate, err := findReviewCandidate(ctx, tx, requestContext, kind, slug)
	if err != nil {
		return ReviewRequest{}, false, err
	}
	if kind == "user" && candidate.ID == requestContext.AuthorID {
		return ReviewRequest{}, false, platform.ErrConflict
	}
	existing, found, err := findExistingReviewRequest(
		ctx, tx, requestContext.MergeRequestID, kind, candidate.ID,
	)
	if err != nil {
		return ReviewRequest{}, false, err
	}
	if found {
		existing.Status, err = reviewCandidateStatus(ctx, tx, requestContext, kind, candidate.ID)
		if err != nil {
			return ReviewRequest{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ReviewRequest{}, false, fmt.Errorf("commit existing review request: %w", err)
		}
		return existing, false, nil
	}
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM merge_request_review_requests
		WHERE merge_request_id = $1 AND removed_at IS NULL
	`, requestContext.MergeRequestID).Scan(&count); err != nil {
		return ReviewRequest{}, false, fmt.Errorf("count review requests: %w", err)
	}
	if count >= maxReviewRequests {
		return ReviewRequest{}, false, ErrReviewRequestLimit
	}
	created := ReviewRequest{
		ID: uuidArg(), Kind: kind, Slug: candidate.Slug, DisplayName: candidate.DisplayName,
		AvatarURL: candidate.AvatarURL, Status: "pending", RequestedBy: actor.Username,
		RequestedAt: nowUTC(),
	}
	created.Status, err = reviewCandidateStatus(ctx, tx, requestContext, kind, candidate.ID)
	if err != nil {
		return ReviewRequest{}, false, err
	}
	var reviewerUserID, reviewerTeamID any
	if kind == "user" {
		reviewerUserID = candidate.ID
	} else {
		reviewerTeamID = candidate.ID
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO merge_request_review_requests (
			id, organization_id, repository_id, merge_request_id,
			reviewer_user_id, reviewer_team_id, requested_by, requested_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, created.ID, requestContext.OrganizationID, repositoryID, requestContext.MergeRequestID,
		reviewerUserID, reviewerTeamID, actor.ID, created.RequestedAt); err != nil {
		return ReviewRequest{}, false, translateConstraintError("create review request", err)
	}
	if err := finishReviewRequestMutation(
		ctx, tx, actor.ID, requestContext, created, "created",
	); err != nil {
		return ReviewRequest{}, false, err
	}
	return created, true, nil
}

func (s *store) RemoveUserReviewRequest(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	username string,
) error {
	return s.removeReviewRequest(ctx, actor, repositoryID, number, "user", username)
}

func (s *store) RemoveTeamReviewRequest(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	team string,
) error {
	return s.removeReviewRequest(ctx, actor, repositoryID, number, "team", team)
}

func (s *store) removeReviewRequest(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	kind string,
	slug string,
) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin review request removal: %w", err)
	}
	defer rollback(ctx, tx)
	requestContext, err := findReviewRequestContext(ctx, tx, actor.ID, repositoryID, number, true)
	if err != nil {
		return err
	}
	if !requestContext.Allowed {
		return platform.ErrForbidden
	}
	request, found, err := findReviewRequestBySlug(
		ctx, tx, requestContext.MergeRequestID, kind, slug,
	)
	if err != nil {
		return err
	}
	if !found {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit missing review request: %w", err)
		}
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE merge_request_review_requests
		SET removed_by = $1, removed_at = $2
		WHERE id = $3 AND removed_at IS NULL
	`, actor.ID, nowUTC(), request.ID); err != nil {
		return fmt.Errorf("remove review request: %w", err)
	}
	return finishReviewRequestMutation(ctx, tx, actor.ID, requestContext, request, "removed")
}

type reviewRequestContext struct {
	MergeRequestID string
	OrganizationID string
	AuthorID       string
	RepositoryID   string
	SourceRevision string
	Allowed        bool
}

func findReviewRequestContext(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repositoryID string,
	number int64,
	lock bool,
) (reviewRequestContext, error) {
	query := `
		SELECT request.id, repository.organization_id, request.author_id, request.source_revision,
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
		             JOIN team_memberships team_member
		               ON team_member.team_id = team.id AND team_member.user_id = actor.id
		               AND team_member.active
		             JOIN organization_memberships organization_member
		               ON organization_member.organization_id = repository.organization_id
		               AND organization_member.user_id = actor.id AND organization_member.active
		             WHERE role.repository_id = repository.id AND role.active
		               AND role.role IN ('triage', 'write', 'maintain', 'admin')
		           )
		           OR (actor.id = request.author_id AND (
		             repository.visibility = 'public'
		             OR EXISTS (
		               SELECT 1 FROM organization_memberships membership
		               WHERE membership.organization_id = repository.organization_id
		                 AND membership.user_id = actor.id AND membership.active
		                 AND repository.visibility = 'internal'
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
		           ))
		         )
		       )
		FROM merge_requests request
		JOIN repositories repository
		  ON repository.id = request.repository_id
		 AND repository.lifecycle_state = 'active'
		 AND repository.archived_at IS NULL AND repository.migrating_at IS NULL
		JOIN organizations organization
		  ON organization.id = repository.organization_id AND organization.active
		WHERE request.repository_id = $1 AND request.number = $2 AND request.state = 'open'`
	if lock {
		query += " FOR UPDATE OF request"
	}
	result := reviewRequestContext{RepositoryID: repositoryID}
	err := tx.QueryRow(ctx, query, repositoryID, number, actorID).Scan(
		&result.MergeRequestID, &result.OrganizationID, &result.AuthorID,
		&result.SourceRevision, &result.Allowed,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return reviewRequestContext{}, platform.ErrNotFound
	}
	if err != nil {
		return reviewRequestContext{}, fmt.Errorf("find pull request for review request: %w", err)
	}
	return result, nil
}

func reviewCandidateStatus(
	ctx context.Context,
	tx pgx.Tx,
	request reviewRequestContext,
	kind string,
	candidateID string,
) (string, error) {
	query := `
		SELECT review.decision
		FROM merge_request_reviews review
		JOIN users reviewer ON reviewer.id = review.reviewer_id AND reviewer.status = 'active'
		WHERE review.merge_request_id = $1 AND review.reviewer_id = $2
		  AND review.source_revision = $3
	`
	args := []any{request.MergeRequestID, candidateID, request.SourceRevision}
	if kind == "team" {
		query = `
			SELECT review.decision
			FROM team_memberships membership
			JOIN organization_memberships organization_member
			  ON organization_member.organization_id = $4
			 AND organization_member.user_id = membership.user_id AND organization_member.active
			JOIN users reviewer ON reviewer.id = membership.user_id AND reviewer.status = 'active'
			JOIN merge_request_reviews review
			  ON review.reviewer_id = membership.user_id AND review.merge_request_id = $1
			 AND review.source_revision = $3
			WHERE membership.team_id = $2 AND membership.active
			ORDER BY CASE review.decision
			  WHEN 'changes_requested' THEN 3
			  WHEN 'approved' THEN 2
			  ELSE 1
			END DESC, review.created_at DESC
			LIMIT 1
		`
		args = append(args, request.OrganizationID)
	}
	var status string
	err := tx.QueryRow(ctx, query, args...).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "pending", nil
	}
	if err != nil {
		return "", fmt.Errorf("find review request status: %w", err)
	}
	return status, nil
}

type reviewCandidateRef struct {
	ID          string
	Slug        string
	DisplayName string
	AvatarURL   string
}

func findReviewCandidate(
	ctx context.Context,
	tx pgx.Tx,
	request reviewRequestContext,
	kind string,
	slug string,
) (reviewCandidateRef, error) {
	if kind == "user" {
		return findUserReviewCandidate(ctx, tx, request, slug)
	}
	return findTeamReviewCandidate(ctx, tx, request, slug)
}

func findUserReviewCandidate(
	ctx context.Context,
	tx pgx.Tx,
	request reviewRequestContext,
	username string,
) (reviewCandidateRef, error) {
	var candidate reviewCandidateRef
	err := tx.QueryRow(ctx, userReviewCandidateSQL+`
		  AND lower(account.username) = lower($2)
	`, request.RepositoryID, username).Scan(
		&candidate.ID, &candidate.Slug, &candidate.DisplayName, &candidate.AvatarURL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return reviewCandidateRef{}, platform.ErrNotFound
	}
	if err != nil {
		return reviewCandidateRef{}, fmt.Errorf("find user review candidate: %w", err)
	}
	return candidate, nil
}

const userReviewCandidateSQL = `
	SELECT account.id, account.username, account.display_name, account.avatar_url
	FROM users account
	JOIN repositories repository
	  ON repository.id = $1 AND repository.lifecycle_state = 'active'
	 AND repository.archived_at IS NULL AND repository.migrating_at IS NULL
	JOIN organizations organization
	  ON organization.id = repository.organization_id AND organization.active
	WHERE account.status = 'active' AND (
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
	    SELECT 1 FROM team_repository_roles role
	    JOIN teams team ON team.id = role.team_id
	      AND team.organization_id = organization.id AND team.active
	    JOIN team_memberships team_member
	      ON team_member.team_id = team.id AND team_member.user_id = account.id
	      AND team_member.active
	    JOIN organization_memberships organization_member
	      ON organization_member.organization_id = organization.id
	      AND organization_member.user_id = account.id AND organization_member.active
	    WHERE role.repository_id = repository.id AND role.active
	  )
	)`

func findTeamReviewCandidate(
	ctx context.Context,
	tx pgx.Tx,
	request reviewRequestContext,
	slug string,
) (reviewCandidateRef, error) {
	var candidate reviewCandidateRef
	err := tx.QueryRow(ctx, teamReviewCandidateSQL+`
		  AND lower(team.slug) = lower($2)
	`, request.RepositoryID, slug).Scan(
		&candidate.ID, &candidate.Slug, &candidate.DisplayName, &candidate.AvatarURL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return reviewCandidateRef{}, platform.ErrNotFound
	}
	if err != nil {
		return reviewCandidateRef{}, fmt.Errorf("find team review candidate: %w", err)
	}
	return candidate, nil
}

const teamReviewCandidateSQL = `
	SELECT team.id, team.slug, team.display_name, ''
	FROM teams team
	JOIN repositories repository
	  ON repository.id = $1 AND repository.organization_id = team.organization_id
	 AND repository.lifecycle_state = 'active'
	 AND repository.archived_at IS NULL AND repository.migrating_at IS NULL
	JOIN organizations organization
	  ON organization.id = team.organization_id AND organization.active
	JOIN team_repository_roles role
	  ON role.team_id = team.id AND role.repository_id = repository.id AND role.active
	WHERE team.active AND EXISTS (
	  SELECT 1 FROM team_memberships member
	  JOIN users account ON account.id = member.user_id AND account.status = 'active'
	  JOIN organization_memberships organization_member
	    ON organization_member.organization_id = team.organization_id
	   AND organization_member.user_id = account.id AND organization_member.active
	  WHERE member.team_id = team.id AND member.active
	)`

func listUserReviewCandidates(
	ctx context.Context,
	tx pgx.Tx,
	request reviewRequestContext,
	query string,
) ([]ReviewCandidate, error) {
	rows, err := tx.Query(ctx, userReviewCandidateSQL+`
		  AND account.id <> $2
		  AND ($3 = '' OR account.username ILIKE '%' || $3 || '%'
		       OR account.display_name ILIKE '%' || $3 || '%')
		ORDER BY lower(account.username), account.id LIMIT 50
	`, request.RepositoryID, request.AuthorID, query)
	if err != nil {
		return nil, fmt.Errorf("list user review candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]ReviewCandidate, 0)
	for rows.Next() {
		var id string
		var candidate ReviewCandidate
		candidate.Kind = "user"
		if err := rows.Scan(&id, &candidate.Slug, &candidate.DisplayName, &candidate.AvatarURL); err != nil {
			return nil, fmt.Errorf("scan user review candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user review candidates: %w", err)
	}
	return candidates, nil
}

func listTeamReviewCandidates(
	ctx context.Context,
	tx pgx.Tx,
	request reviewRequestContext,
	query string,
) ([]ReviewCandidate, error) {
	rows, err := tx.Query(ctx, teamReviewCandidateSQL+`
		  AND ($2 = '' OR team.slug ILIKE '%' || $2 || '%'
		       OR team.display_name ILIKE '%' || $2 || '%')
		ORDER BY lower(team.slug), team.id LIMIT 50
	`, request.RepositoryID, query)
	if err != nil {
		return nil, fmt.Errorf("list team review candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]ReviewCandidate, 0)
	for rows.Next() {
		var id string
		var candidate ReviewCandidate
		candidate.Kind = "team"
		if err := rows.Scan(&id, &candidate.Slug, &candidate.DisplayName, &candidate.AvatarURL); err != nil {
			return nil, fmt.Errorf("scan team review candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate team review candidates: %w", err)
	}
	return candidates, nil
}

func findExistingReviewRequest(
	ctx context.Context,
	tx pgx.Tx,
	mergeRequestID string,
	kind string,
	candidateID string,
) (ReviewRequest, bool, error) {
	column := "reviewer_user_id"
	if kind == "team" {
		column = "reviewer_team_id"
	}
	query := fmt.Sprintf(`
		SELECT request.id, $3, COALESCE(reviewer.username, team.slug),
		       COALESCE(reviewer.display_name, team.display_name),
		       COALESCE(reviewer.avatar_url, ''), 'pending', requester.username, request.requested_at
		FROM merge_request_review_requests request
		LEFT JOIN users reviewer ON reviewer.id = request.reviewer_user_id
		LEFT JOIN teams team ON team.id = request.reviewer_team_id
		JOIN users requester ON requester.id = request.requested_by
		WHERE request.merge_request_id = $1 AND request.%s = $2 AND request.removed_at IS NULL
	`, column)
	var request ReviewRequest
	err := tx.QueryRow(ctx, query, mergeRequestID, candidateID, kind).Scan(
		&request.ID, &request.Kind, &request.Slug, &request.DisplayName,
		&request.AvatarURL, &request.Status, &request.RequestedBy, &request.RequestedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReviewRequest{}, false, nil
	}
	if err != nil {
		return ReviewRequest{}, false, fmt.Errorf("find existing review request: %w", err)
	}
	return request, true, nil
}

func findReviewRequestBySlug(
	ctx context.Context,
	tx pgx.Tx,
	mergeRequestID string,
	kind string,
	slug string,
) (ReviewRequest, bool, error) {
	predicate := "request.reviewer_user_id = reviewer.id AND lower(reviewer.username) = lower($2)"
	if kind == "team" {
		predicate = "request.reviewer_team_id = team.id AND lower(team.slug) = lower($2)"
	}
	query := fmt.Sprintf(`
		SELECT request.id, $3, COALESCE(reviewer.username, team.slug),
		       COALESCE(reviewer.display_name, team.display_name),
		       COALESCE(reviewer.avatar_url, ''), 'pending', requester.username, request.requested_at
		FROM merge_request_review_requests request
		LEFT JOIN users reviewer ON reviewer.id = request.reviewer_user_id
		LEFT JOIN teams team ON team.id = request.reviewer_team_id
		JOIN users requester ON requester.id = request.requested_by
		WHERE request.merge_request_id = $1 AND request.removed_at IS NULL AND %s
		FOR UPDATE OF request
	`, predicate)
	var request ReviewRequest
	err := tx.QueryRow(ctx, query, mergeRequestID, slug, kind).Scan(
		&request.ID, &request.Kind, &request.Slug, &request.DisplayName,
		&request.AvatarURL, &request.Status, &request.RequestedBy, &request.RequestedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReviewRequest{}, false, nil
	}
	if err != nil {
		return ReviewRequest{}, false, fmt.Errorf("find review request for removal: %w", err)
	}
	return request, true, nil
}

func finishReviewRequestMutation(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	requestContext reviewRequestContext,
	request ReviewRequest,
	change string,
) error {
	if _, err := tx.Exec(ctx, `
		UPDATE merge_requests SET updated_at = $1
		WHERE id = $2 AND repository_id = $3
	`, nowUTC(), requestContext.MergeRequestID, requestContext.RepositoryID); err != nil {
		return fmt.Errorf("update pull request review request timestamp: %w", err)
	}
	if err := insertAudit(
		ctx, tx, actorID, requestContext.OrganizationID, requestContext.RepositoryID,
		"merge_request.review_request."+change, "merge_request_review_request", request.ID,
	); err != nil {
		return err
	}
	payload := map[string]any{
		"mergeRequestId": requestContext.MergeRequestID,
		"repositoryId":   requestContext.RepositoryID,
		"change":         change,
		"reviewRequest":  request,
	}
	if err := insertOutbox(
		ctx, tx, "merge_request_review_request."+change, request.ID+":"+uuidArg(), payload,
	); err != nil {
		return err
	}
	if change == "created" {
		if err := RecordWorkItemEvent(ctx, tx, WorkItemEventRecord{
			RepositoryID: requestContext.RepositoryID, ItemKind: WorkItemMergeRequest,
			ItemID: requestContext.MergeRequestID, ActorID: actorID, Kind: EventReviewRequested,
			Payload: WorkItemEventPayload{Reviewer: request.Slug},
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit review request mutation: %w", err)
	}
	return nil
}
