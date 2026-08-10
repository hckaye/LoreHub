package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/auth"
)

var (
	ErrNotFound      = errors.New("resource not found")
	ErrForbidden     = errors.New("operation is not permitted")
	ErrConflict      = errors.New("resource already exists")
	slugPattern      = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
	partitionPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (store *Store) ActiveUser(ctx context.Context, userID string) (User, error) {
	var user User
	err := store.pool.QueryRow(ctx, `
		SELECT id, username, display_name, COALESCE(email, ''), locale
		FROM users
		WHERE id = $1 AND status = 'active'
	`, userID).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Email, &user.Locale)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrForbidden
	}
	if err != nil {
		return User{}, fmt.Errorf("find active user: %w", err)
	}
	return user, nil
}

func (store *Store) EnsureUser(ctx context.Context, principal auth.Principal) (User, error) {
	var user User
	var status string
	err := store.pool.QueryRow(ctx, `
		SELECT u.id, u.username, u.display_name, COALESCE(u.email, ''), u.locale, u.status
		FROM user_identities i
		JOIN users u ON u.id = i.user_id
		WHERE i.issuer = $1 AND i.subject = $2
	`, principal.Issuer, principal.Subject).Scan(
		&user.ID,
		&user.Username,
		&user.DisplayName,
		&user.Email,
		&user.Locale,
		&status,
	)
	if err == nil {
		if status != "active" {
			return User{}, ErrForbidden
		}
		displayName := strings.TrimSpace(principal.Name)
		if displayName == "" {
			displayName = user.DisplayName
		}
		displayName = limitText(displayName, 160)
		email := user.Email
		if strings.TrimSpace(principal.Email) != "" {
			email = limitText(strings.ToLower(strings.TrimSpace(principal.Email)), 320)
		}
		locale := user.Locale
		if strings.TrimSpace(principal.PreferredLocale) != "" {
			locale = normalizedLocale(principal.PreferredLocale)
		}
		if err := store.pool.QueryRow(ctx, `
			UPDATE users
			SET display_name = $2, email = NULLIF($3, ''), locale = $4, updated_at = now()
			WHERE id = $1 AND status = 'active'
			RETURNING id, username, display_name, COALESCE(email, ''), locale
		`, user.ID, displayName, email, locale).Scan(
			&user.ID,
			&user.Username,
			&user.DisplayName,
			&user.Email,
			&user.Locale,
		); err != nil {
			return User{}, fmt.Errorf("update user profile: %w", err)
		}
		if _, err := store.pool.Exec(ctx, `
			UPDATE user_identities
			SET last_seen_at = now()
			WHERE issuer = $1 AND subject = $2
		`, principal.Issuer, principal.Subject); err != nil {
			return User{}, fmt.Errorf("update user identity: %w", err)
		}
		return user, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return User{}, fmt.Errorf("find user identity: %w", err)
	}
	return store.createUser(ctx, principal)
}

func (store *Store) createUser(ctx context.Context, principal auth.Principal) (User, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return User{}, fmt.Errorf("begin user provisioning: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	userID := uuid.New()
	username := normalizedUsername(principal.Username, userID.String())
	displayName := strings.TrimSpace(principal.Name)
	if displayName == "" {
		displayName = username
	}
	displayName = limitText(displayName, 160)
	locale := normalizedLocale(principal.PreferredLocale)
	var email any
	if strings.TrimSpace(principal.Email) != "" {
		email = limitText(strings.ToLower(strings.TrimSpace(principal.Email)), 320)
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO users (id, username, display_name, email, locale)
		VALUES ($1, $2, $3, $4, $5)
	`, userID, username, displayName, email, locale)
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO user_identities (id, user_id, issuer, subject)
		VALUES ($1, $2, $3, $4)
	`, uuid.New(), userID, principal.Issuer, principal.Subject)
	if err != nil {
		return User{}, fmt.Errorf("create user identity: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit user provisioning: %w", err)
	}
	return User{
		ID:          userID.String(),
		Username:    username,
		DisplayName: displayName,
		Email:       principal.Email,
		Locale:      locale,
	}, nil
}

func (store *Store) CreateOrganization(
	ctx context.Context,
	actor User,
	input CreateOrganizationInput,
) (Organization, error) {
	if _, err := store.ActiveUser(ctx, actor.ID); err != nil {
		return Organization{}, err
	}
	if err := validateSlug(input.Slug); err != nil {
		return Organization{}, err
	}
	organization := Organization{
		ID:          uuid.NewString(),
		Slug:        input.Slug,
		DisplayName: input.DisplayName,
		Description: input.Description,
		Visibility:  input.Visibility,
		CreatedAt:   time.Now().UTC(),
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Organization{}, fmt.Errorf("begin organization transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	_, err = transaction.Exec(ctx, `
		INSERT INTO organizations (
			id, slug, display_name, description, visibility, created_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
	`, organization.ID, organization.Slug, organization.DisplayName, organization.Description,
		organization.Visibility, actor.ID, organization.CreatedAt)
	if err != nil {
		return Organization{}, translateConstraintError("create organization", err)
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, organization.ID, actor.ID)
	if err != nil {
		return Organization{}, fmt.Errorf("create organization owner: %w", err)
	}
	if err := insertAudit(ctx, transaction, actor.ID, organization.ID, "", "organization.create", "organization",
		organization.ID); err != nil {
		return Organization{}, err
	}
	if err := insertOutbox(ctx, transaction, "organization.created", organization.ID, organization); err != nil {
		return Organization{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return Organization{}, fmt.Errorf("commit organization transaction: %w", err)
	}
	return organization, nil
}

func (store *Store) RegisterRepository(
	ctx context.Context,
	actor User,
	organizationSlug string,
	input RegisterRepositoryInput,
) (Repository, error) {
	if err := validateSlug(input.Slug); err != nil {
		return Repository{}, err
	}
	var organizationID string
	var role string
	err := store.pool.QueryRow(ctx, `
		SELECT o.id, m.role
		FROM organizations o
		JOIN organization_memberships m ON m.organization_id = o.id AND m.active
		JOIN users u ON u.id = m.user_id AND u.status = 'active'
		WHERE o.slug = $1 AND m.user_id = $2
	`, organizationSlug, actor.ID).Scan(&organizationID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return Repository{}, ErrForbidden
	}
	if err != nil {
		return Repository{}, fmt.Errorf("check organization permission: %w", err)
	}
	if role != "owner" && role != "maintainer" {
		return Repository{}, ErrForbidden
	}
	if !isLorePartitionID(input.LoreRepositoryID) {
		return Repository{}, errors.New("Lore repository ID must be the canonical 32-character partition ID")
	}
	if err := validatePublicLoreURL(input.LoreURL); err != nil {
		return Repository{}, err
	}

	repository := Repository{
		ID:               uuid.NewString(),
		OrganizationID:   organizationID,
		Owner:            organizationSlug,
		Slug:             input.Slug,
		DisplayName:      input.DisplayName,
		Description:      input.Description,
		Visibility:       input.Visibility,
		LoreRepositoryID: input.LoreRepositoryID,
		LoreURL:          input.LoreURL,
		DefaultBranch:    input.DefaultBranch,
		UpdatedAt:        time.Now().UTC(),
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Repository{}, fmt.Errorf("begin repository transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	_, err = transaction.Exec(ctx, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
	`, repository.ID, repository.OrganizationID, repository.Slug, repository.DisplayName,
		repository.Description, repository.Visibility, repository.LoreRepositoryID, repository.LoreURL,
		repository.DefaultBranch, actor.ID, repository.UpdatedAt)
	if err != nil {
		return Repository{}, translateConstraintError("register repository", err)
	}
	if _, err := transaction.Exec(ctx, `
		INSERT INTO repository_counters (repository_id) VALUES ($1)
	`, repository.ID); err != nil {
		return Repository{}, fmt.Errorf("create repository counters: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		INSERT INTO repository_policies (repository_id) VALUES ($1)
	`, repository.ID); err != nil {
		return Repository{}, fmt.Errorf("create repository policy: %w", err)
	}
	if repository.Visibility == "public" {
		if err := addAnonymousReaderGrant(ctx, transaction, actor.ID, organizationID, repository.ID); err != nil {
			return Repository{}, err
		}
	}
	if err := addCIReadGrant(ctx, transaction, actor.ID, organizationID, repository.ID); err != nil {
		return Repository{}, err
	}
	if err := addObserverReadGrant(ctx, transaction, actor.ID, organizationID, repository.ID); err != nil {
		return Repository{}, err
	}
	if err := insertAudit(ctx, transaction, actor.ID, organizationID, repository.ID, "repository.register",
		"repository", repository.ID); err != nil {
		return Repository{}, err
	}
	if err := insertOutbox(ctx, transaction, "repository.registered", repository.ID, repository); err != nil {
		return Repository{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return Repository{}, fmt.Errorf("commit repository transaction: %w", err)
	}
	return repository, nil
}

func (store *Store) ExploreRepositories(ctx context.Context, limit int) ([]Repository, error) {
	if limit < 1 || limit > 100 {
		limit = 30
	}
	rows, err := store.pool.Query(ctx, repositorySelect+`
		WHERE r.visibility = 'public' AND r.archived_at IS NULL AND r.lifecycle_state = 'active'
		GROUP BY r.id, o.slug
		ORDER BY r.updated_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list public repositories: %w", err)
	}
	defer rows.Close()
	return scanRepositories(rows)
}

func (store *Store) PublicRepository(ctx context.Context, owner string, slug string) (Repository, error) {
	row := store.pool.QueryRow(ctx, repositorySelect+`
		WHERE o.slug = $1 AND r.slug = $2 AND o.active
		  AND r.visibility = 'public' AND r.archived_at IS NULL
		  AND r.lifecycle_state = 'active'
		GROUP BY r.id, o.slug
	`, owner, slug)
	repository, err := scanRepository(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Repository{}, ErrNotFound
	}
	if err != nil {
		return Repository{}, fmt.Errorf("get public repository: %w", err)
	}
	return repository, nil
}

func (store *Store) RepositoryForRead(
	ctx context.Context,
	actor *User,
	owner string,
	slug string,
) (Repository, error) {
	if actor == nil {
		return store.PublicRepository(ctx, owner, slug)
	}
	row := store.pool.QueryRow(ctx, repositorySelect+`
		JOIN users actor_user ON actor_user.id = $3 AND actor_user.status = 'active'
		WHERE o.slug = $1 AND r.slug = $2 AND r.archived_at IS NULL
		  AND r.lifecycle_state = 'active'
			AND (
			      r.visibility = 'public'
			      OR r.visibility = 'internal'
			      AND EXISTS (
			          SELECT 1 FROM organization_memberships iom
			          WHERE iom.organization_id = o.id AND iom.user_id = $3 AND iom.active
			      )
				  OR EXISTS (
			      SELECT 1 FROM organization_memberships om
			      JOIN users u ON u.id = om.user_id AND u.status = 'active'
			      WHERE om.organization_id = o.id AND om.user_id = $3 AND om.active
				        AND om.role = 'owner'
			  )
			      OR EXISTS (
			          SELECT 1 FROM repository_memberships rm
			          WHERE rm.repository_id = r.id AND rm.user_id = $3 AND rm.active
			      )
		      OR EXISTS (
		          SELECT 1
		          FROM team_repository_roles tr
		          JOIN teams t ON t.id = tr.team_id AND t.organization_id = o.id
		          JOIN team_memberships tm ON tm.team_id = t.id AND tm.user_id = $3 AND tm.active
		          JOIN organization_memberships om
		            ON om.organization_id = o.id AND om.user_id = $3 AND om.active
			          WHERE tr.repository_id = r.id AND tr.active AND t.active
		      )
		  )
		GROUP BY r.id, o.slug
	`, owner, slug, actor.ID)
	repository, err := scanRepository(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Repository{}, ErrNotFound
	}
	if err != nil {
		return Repository{}, fmt.Errorf("get readable repository: %w", err)
	}
	return repository, nil
}

func (store *Store) RepositoryForWrite(
	ctx context.Context,
	actor User,
	owner string,
	slug string,
) (Repository, error) {
	row := store.pool.QueryRow(ctx, repositorySelect+`
		JOIN users actor_user ON actor_user.id = $3 AND actor_user.status = 'active'
		WHERE o.slug = $1 AND r.slug = $2 AND r.archived_at IS NULL
		  AND r.lifecycle_state = 'active'
		  AND (
			  EXISTS (
			          SELECT 1 FROM repository_memberships rm
			          WHERE rm.repository_id = r.id AND rm.user_id = $3 AND rm.active
		            AND rm.role IN ('admin', 'maintain', 'write')
		      )
			      OR EXISTS (
			          SELECT 1 FROM organization_memberships om
			          WHERE om.organization_id = o.id AND om.user_id = $3
			            AND om.active AND om.role = 'owner'
			      )
		      OR EXISTS (
		          SELECT 1
		          FROM team_repository_roles tr
		          JOIN teams t ON t.id = tr.team_id AND t.organization_id = o.id
		          JOIN team_memberships tm ON tm.team_id = t.id AND tm.user_id = $3 AND tm.active
		          JOIN organization_memberships om
		            ON om.organization_id = o.id AND om.user_id = $3 AND om.active
			          WHERE tr.repository_id = r.id AND tr.active AND t.active
			            AND tr.role IN ('admin', 'maintain', 'write')
		      )
		  )
		GROUP BY r.id, o.slug
	`, owner, slug, actor.ID)
	repository, err := scanRepository(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Repository{}, ErrNotFound
	}
	if err != nil {
		return Repository{}, fmt.Errorf("get writable repository: %w", err)
	}
	return repository, nil
}

func (store *Store) ListPublicIssues(
	ctx context.Context,
	owner string,
	slug string,
	state string,
) ([]Issue, error) {
	if state != "open" && state != "closed" {
		state = "open"
	}
	rows, err := store.pool.Query(ctx, `
		SELECT i.id, i.number, i.title, i.body, i.state, author.username,
		       assignee.username, COUNT(c.id), i.created_at, i.updated_at
		FROM issues i
		JOIN repositories r ON r.id = i.repository_id
		JOIN organizations o ON o.id = r.organization_id AND o.active
		JOIN users author ON author.id = i.author_id
		LEFT JOIN users assignee ON assignee.id = i.assignee_id
		LEFT JOIN issue_comments c ON c.issue_id = i.id
		WHERE o.slug = $1 AND r.slug = $2 AND o.active
		  AND r.visibility = 'public' AND r.lifecycle_state = 'active' AND i.state = $3
		GROUP BY i.id, author.username, assignee.username
		ORDER BY i.updated_at DESC
		LIMIT 100
	`, owner, slug, state)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	defer rows.Close()
	issues := make([]Issue, 0)
	for rows.Next() {
		var issue Issue
		if err := rows.Scan(&issue.ID, &issue.Number, &issue.Title, &issue.Body, &issue.State, &issue.Author,
			&issue.Assignee, &issue.CommentCount, &issue.CreatedAt, &issue.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan issue: %w", err)
		}
		issues = append(issues, issue)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate issues: %w", err)
	}
	return issues, nil
}

func (store *Store) CreateIssue(
	ctx context.Context,
	actor User,
	owner string,
	slug string,
	input CreateIssueInput,
) (Issue, error) {
	repositoryID, organizationID, err := store.writableRepository(ctx, actor.ID, owner, slug)
	if err != nil {
		return Issue{}, err
	}

	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Issue{}, fmt.Errorf("begin issue transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	var number int64
	err = transaction.QueryRow(ctx, `
		UPDATE repository_counters
		SET next_issue_number = next_issue_number + 1
		WHERE repository_id = $1
		RETURNING next_issue_number - 1
	`, repositoryID).Scan(&number)
	if err != nil {
		return Issue{}, fmt.Errorf("allocate issue number: %w", err)
	}
	issue := Issue{
		ID:        uuid.NewString(),
		Number:    number,
		Title:     strings.TrimSpace(input.Title),
		Body:      input.Body,
		State:     "open",
		Author:    actor.Username,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO issues (
			id, repository_id, number, title, body, state, author_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, issue.ID, repositoryID, issue.Number, issue.Title, issue.Body, issue.State, actor.ID,
		issue.CreatedAt, issue.UpdatedAt)
	if err != nil {
		return Issue{}, fmt.Errorf("create issue: %w", err)
	}
	if err := insertAudit(ctx, transaction, actor.ID, organizationID, repositoryID, "issue.create", "issue",
		issue.ID); err != nil {
		return Issue{}, err
	}
	if err := insertOutbox(ctx, transaction, "issue.created", issue.ID, issue); err != nil {
		return Issue{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return Issue{}, fmt.Errorf("commit issue transaction: %w", err)
	}
	return issue, nil
}

func (store *Store) ListPublicMergeRequests(
	ctx context.Context,
	owner string,
	slug string,
	state string,
) ([]MergeRequest, error) {
	if state != "open" && state != "closed" && state != "merged" {
		state = "open"
	}
	rows, err := store.pool.Query(ctx, `
		SELECT mr.id, mr.number, mr.title, mr.body, mr.state,
		       mr.source_branch, mr.target_branch, mr.source_revision, mr.target_revision,
		       author.username,
		       COUNT(review.id) FILTER (WHERE review.decision = 'approved'),
		       mr.created_at, mr.updated_at
		FROM merge_requests mr
		JOIN repositories r ON r.id = mr.repository_id
		JOIN organizations o ON o.id = r.organization_id AND o.active
		JOIN users author ON author.id = mr.author_id
		LEFT JOIN merge_request_reviews review ON review.merge_request_id = mr.id
		WHERE o.slug = $1 AND r.slug = $2 AND o.active
		  AND r.visibility = 'public' AND r.lifecycle_state = 'active' AND mr.state = $3
		GROUP BY mr.id, author.username
		ORDER BY mr.updated_at DESC
		LIMIT 100
	`, owner, slug, state)
	if err != nil {
		return nil, fmt.Errorf("list merge requests: %w", err)
	}
	defer rows.Close()
	mergeRequests := make([]MergeRequest, 0)
	for rows.Next() {
		var mergeRequest MergeRequest
		if err := rows.Scan(
			&mergeRequest.ID,
			&mergeRequest.Number,
			&mergeRequest.Title,
			&mergeRequest.Body,
			&mergeRequest.State,
			&mergeRequest.SourceBranch,
			&mergeRequest.TargetBranch,
			&mergeRequest.SourceRevision,
			&mergeRequest.TargetRevision,
			&mergeRequest.Author,
			&mergeRequest.ApprovalCount,
			&mergeRequest.CreatedAt,
			&mergeRequest.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan merge request: %w", err)
		}
		mergeRequests = append(mergeRequests, mergeRequest)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate merge requests: %w", err)
	}
	return mergeRequests, nil
}

func (store *Store) CreateMergeRequest(
	ctx context.Context,
	actor User,
	owner string,
	slug string,
	input CreateMergeRequestInput,
) (MergeRequest, error) {
	repositoryID, organizationID, err := store.writableRepository(ctx, actor.ID, owner, slug)
	if err != nil {
		return MergeRequest{}, err
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return MergeRequest{}, fmt.Errorf("begin merge request transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	var number int64
	err = transaction.QueryRow(ctx, `
		UPDATE repository_counters
		SET next_merge_request_number = next_merge_request_number + 1
		WHERE repository_id = $1
		RETURNING next_merge_request_number - 1
	`, repositoryID).Scan(&number)
	if err != nil {
		return MergeRequest{}, fmt.Errorf("allocate merge request number: %w", err)
	}
	mergeRequest := MergeRequest{
		ID:             uuid.NewString(),
		Number:         number,
		Title:          strings.TrimSpace(input.Title),
		Body:           input.Body,
		State:          "open",
		SourceBranch:   input.SourceBranch,
		TargetBranch:   input.TargetBranch,
		SourceRevision: input.SourceRevision,
		TargetRevision: input.TargetRevision,
		Author:         actor.Username,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO merge_requests (
			id, repository_id, number, title, body, state, source_branch, target_branch,
			source_revision, target_revision, author_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`, mergeRequest.ID, repositoryID, mergeRequest.Number, mergeRequest.Title, mergeRequest.Body,
		mergeRequest.State, mergeRequest.SourceBranch, mergeRequest.TargetBranch,
		mergeRequest.SourceRevision, mergeRequest.TargetRevision, actor.ID,
		mergeRequest.CreatedAt, mergeRequest.UpdatedAt)
	if err != nil {
		return MergeRequest{}, fmt.Errorf("create merge request: %w", err)
	}
	if err := insertAudit(ctx, transaction, actor.ID, organizationID, repositoryID, "merge_request.create",
		"merge_request", mergeRequest.ID); err != nil {
		return MergeRequest{}, err
	}
	if err := insertOutbox(ctx, transaction, "merge_request.created", mergeRequest.ID, mergeRequest); err != nil {
		return MergeRequest{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return MergeRequest{}, fmt.Errorf("commit merge request transaction: %w", err)
	}
	return mergeRequest, nil
}

func (store *Store) ListPublicCIRuns(
	ctx context.Context,
	owner string,
	slug string,
) ([]CIRun, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT run.id, run.run_number, run.event_name, run.branch, run.revision,
		       run.status, run.conclusion, run.queued_at, run.started_at, run.completed_at
		FROM ci_runs run
		JOIN repositories r ON r.id = run.repository_id
		JOIN organizations o ON o.id = r.organization_id AND o.active
		WHERE o.slug = $1 AND r.slug = $2 AND o.active
		  AND r.visibility = 'public' AND r.lifecycle_state = 'active'
		ORDER BY run.run_number DESC
		LIMIT 50
	`, owner, slug)
	if err != nil {
		return nil, fmt.Errorf("list CI runs: %w", err)
	}
	defer rows.Close()
	runs := make([]CIRun, 0)
	for rows.Next() {
		var run CIRun
		if err := rows.Scan(
			&run.ID,
			&run.RunNumber,
			&run.EventName,
			&run.Branch,
			&run.Revision,
			&run.Status,
			&run.Conclusion,
			&run.QueuedAt,
			&run.StartedAt,
			&run.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan CI run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate CI runs: %w", err)
	}
	return runs, nil
}

func (store *Store) writableRepository(
	ctx context.Context,
	userID string,
	owner string,
	slug string,
) (string, string, error) {
	var repositoryID string
	var organizationID string
	var allowed bool
	err := store.pool.QueryRow(ctx, `
		SELECT r.id, r.organization_id,
		       EXISTS (
		           SELECT 1
		           FROM users actor_user
		           WHERE actor_user.id = $3 AND actor_user.status = 'active'
		             AND (
		                 EXISTS (
		                     SELECT 1 FROM repository_memberships rm
		                     WHERE rm.repository_id = r.id AND rm.user_id = $3 AND rm.active
		                       AND rm.role IN ('admin', 'maintain', 'write')
		                 )
			                 OR EXISTS (
			                     SELECT 1 FROM organization_memberships om
			                     WHERE om.organization_id = o.id AND om.user_id = $3
			                       AND om.active AND om.role = 'owner'
			                 )
		                 OR EXISTS (
		                     SELECT 1
		                     FROM team_repository_roles tr
		                     JOIN teams t ON t.id = tr.team_id AND t.organization_id = o.id
		                     JOIN team_memberships tm
		                       ON tm.team_id = t.id AND tm.user_id = $3 AND tm.active
		                     JOIN organization_memberships om
		                       ON om.organization_id = o.id AND om.user_id = $3 AND om.active
			                     WHERE tr.repository_id = r.id AND tr.active AND t.active
			                       AND tr.role IN ('admin', 'maintain', 'write')
		                 )
		             )
		       )
		FROM repositories r
		JOIN organizations o ON o.id = r.organization_id AND o.active
		WHERE o.slug = $1 AND r.slug = $2 AND o.active
		  AND r.archived_at IS NULL AND r.lifecycle_state = 'active'
	`, owner, slug, userID).Scan(&repositoryID, &organizationID, &allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("check repository write permission: %w", err)
	}
	if !allowed {
		return "", "", ErrForbidden
	}
	return repositoryID, organizationID, nil
}

func (store *Store) canReadRepository(
	ctx context.Context,
	userID string,
	repositoryID string,
	organizationID string,
) (bool, error) {
	var allowed bool
	err := store.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM users actor_user
			JOIN repositories readable
			  ON readable.id = $1 AND readable.organization_id = $3
			 AND readable.lifecycle_state = 'active'
			JOIN organizations readable_organization
			  ON readable_organization.id = readable.organization_id AND readable_organization.active
			WHERE actor_user.id = $2 AND actor_user.status = 'active'
			  AND (
				  EXISTS (
					  SELECT 1 FROM organization_memberships
					  WHERE organization_id = $3 AND user_id = $2 AND active
					    AND readable.visibility IN ('internal', 'public')
				  )
				  OR
				  EXISTS (
					  SELECT 1
						  FROM repository_memberships rm
						  WHERE rm.repository_id = $1 AND rm.user_id = $2 AND rm.active
				  )
					  OR EXISTS (
						  SELECT 1 FROM organization_memberships
						  WHERE organization_id = $3 AND user_id = $2 AND active
						    AND role = 'owner'
					  )
				  OR EXISTS (
					  SELECT 1
					  FROM team_repository_roles tr
					  JOIN teams t ON t.id = tr.team_id AND t.organization_id = $3
					  JOIN team_memberships tm
					    ON tm.team_id = t.id AND tm.user_id = $2 AND tm.active
					  JOIN organization_memberships om
					    ON om.organization_id = $3 AND om.user_id = $2 AND om.active
					  WHERE tr.repository_id = $1 AND tr.active AND t.active
				  )
			  )
		)
	`, repositoryID, userID, organizationID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("check repository permission: %w", err)
	}
	return allowed, nil
}

const repositorySelect = `
		SELECT r.id, r.organization_id, o.slug, r.slug, r.display_name, r.description,
		       r.visibility, r.lore_repository_id, r.lore_url, r.default_branch,
		       r.homepage_url, r.allow_issues, r.allow_merge_requests,
		       COUNT(DISTINCT i.id) FILTER (WHERE i.state = 'open'),
		       COUNT(DISTINCT mr.id) FILTER (WHERE mr.state = 'open'),
		       r.lifecycle_state, COALESCE(r.provisioning_error, ''), r.updated_at
	FROM repositories r
	JOIN organizations o ON o.id = r.organization_id AND o.active
	LEFT JOIN issues i ON i.repository_id = r.id
	LEFT JOIN merge_requests mr ON mr.repository_id = r.id
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRepository(row rowScanner) (Repository, error) {
	var repository Repository
	err := row.Scan(
		&repository.ID,
		&repository.OrganizationID,
		&repository.Owner,
		&repository.Slug,
		&repository.DisplayName,
		&repository.Description,
		&repository.Visibility,
		&repository.LoreRepositoryID,
		&repository.LoreURL,
		&repository.DefaultBranch,
		&repository.HomepageURL,
		&repository.AllowIssues,
		&repository.AllowMergeRequests,
		&repository.IssueCount,
		&repository.MergeRequestCount,
		&repository.LifecycleState,
		&repository.ProvisioningError,
		&repository.UpdatedAt,
	)
	return repository, err
}

func scanRepositories(rows pgx.Rows) ([]Repository, error) {
	repositories := make([]Repository, 0)
	for rows.Next() {
		repository, err := scanRepository(rows)
		if err != nil {
			return nil, fmt.Errorf("scan repository: %w", err)
		}
		repositories = append(repositories, repository)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repositories: %w", err)
	}
	return repositories, nil
}

func isLorePartitionID(value string) bool {
	return partitionPattern.MatchString(value)
}

func validateSlug(slug string) error {
	if !slugPattern.MatchString(slug) {
		return errors.New("slug must contain lowercase letters, numbers, or internal hyphens")
	}
	return nil
}

func normalizedUsername(value string, fallbackID string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
			builder.WriteRune(character)
		}
	}
	username := strings.Trim(builder.String(), "-")
	if username == "" {
		username = "user-" + fallbackID[:8]
	}
	if len(username) > 63 {
		username = strings.Trim(username[:63], "-")
	}
	return username
}

func normalizedLocale(locale string) string {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if strings.HasPrefix(locale, "ja") {
		return "ja"
	}
	return "en"
}

func limitText(value string, limit int) string {
	characters := []rune(value)
	if len(characters) <= limit {
		return value
	}
	return string(characters[:limit])
}

func translateConstraintError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%s: %w", operation, ErrConflict)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func insertAudit(
	ctx context.Context,
	transaction pgx.Tx,
	actorID string,
	organizationID string,
	repositoryID string,
	action string,
	targetType string,
	targetID string,
) error {
	_, err := transaction.Exec(ctx, `
		INSERT INTO audit_events (
			id, organization_id, repository_id, actor_id, action, target_type, target_id
		) VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4, $5, $6, $7)
	`, uuid.New(), organizationID, repositoryID, actorID, action, targetType, targetID)
	if err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}
	return nil
}

func insertOutbox(
	ctx context.Context,
	transaction pgx.Tx,
	topic string,
	eventKey string,
	payload any,
) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode outbox payload: %w", err)
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, $2, $3, $4)
	`, uuid.New(), topic, eventKey, encoded)
	if err != nil {
		return fmt.Errorf("record outbox event: %w", err)
	}
	return nil
}
