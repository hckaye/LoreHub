package collab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

// UserEnsurer provisions or loads the local user for an authenticated principal.
type UserEnsurer interface {
	EnsureUser(ctx context.Context, principal auth.Principal) (platform.User, error)
}

// Store is the persistence contract used by collaboration handlers. The
// interface is intentionally narrow so handlers can be tested with fakes while
// the concrete implementation owns all SQL.
type Store interface {
	UserEnsurer
	LookupRepository(
		ctx context.Context, actor *platform.User, owner, slug string,
	) (Repository, error)
	RepositoryPermission(
		ctx context.Context, actor platform.User, repo Repository,
	) (Access, error)

	GetIssue(ctx context.Context, repoID string, number int64) (Issue, error)
	UpdateIssue(
		ctx context.Context, actor platform.User, repoID string,
		number int64, input UpdateIssueInput,
	) (Issue, error)

	ListIssueComments(
		ctx context.Context, repoID string, number int64, page Page,
	) (Result[IssueComment], error)
	CreateIssueComment(
		ctx context.Context, actor platform.User, repoID string,
		number int64, body string,
	) (IssueComment, error)
	UpdateIssueComment(
		ctx context.Context, actor platform.User, repoID string, issueNumber int64,
		commentID, body string,
	) (IssueComment, error)
	DeleteIssueComment(
		ctx context.Context, actor platform.User, repoID string, issueNumber int64, commentID string,
	) error

	ListLabels(ctx context.Context, repoID string, page Page) (Result[Label], error)
	CreateLabel(
		ctx context.Context, actor platform.User, repoID string, input LabelInput,
	) (Label, error)
	UpdateLabel(
		ctx context.Context, actor platform.User, repoID, labelID string, input LabelInput,
	) (Label, error)
	DeleteLabel(ctx context.Context, actor platform.User, repoID, labelID string) error
	ApplyLabel(
		ctx context.Context, actor platform.User, repoID string,
		issueNumber int64, labelID string,
	) (Label, bool, error)
	RemoveLabel(
		ctx context.Context, actor platform.User, repoID string,
		issueNumber int64, labelID string,
	) error

	GetMergeRequest(ctx context.Context, repoID string, number int64) (MergeRequest, error)
	UpdateMergeRequest(
		ctx context.Context, actor platform.User, repoID string,
		number int64, input UpdateMergeRequestInput,
	) (MergeRequest, error)
	ListReviews(ctx context.Context, repoID string, number int64) (ReviewSummary, error)
	CreateReview(
		ctx context.Context, actor platform.User, repoID string,
		number int64, input ReviewInput,
	) (Review, bool, error)

	ListBranchRules(ctx context.Context, repoID string) ([]BranchRule, error)
	CreateBranchRule(
		ctx context.Context, actor platform.User, repoID string, input BranchRuleInput,
	) (BranchRule, error)
	UpdateBranchRule(
		ctx context.Context, actor platform.User, repoID, ruleID string, input BranchRuleInput,
	) (BranchRule, error)
	DeleteBranchRule(ctx context.Context, actor platform.User, repoID, ruleID string) error
}

// store is the concrete PostgreSQL-backed implementation of Store.
type store struct {
	pool    *pgxpool.Pool
	ensurer UserEnsurer
}

// NewStore wraps a connection pool. User provisioning delegates to the existing
// platform store so identity behavior stays consistent with the rest of API.
func NewStore(pool *pgxpool.Pool) Store {
	return &store{pool: pool, ensurer: platform.NewStore(pool)}
}

func (s *store) EnsureUser(ctx context.Context, principal auth.Principal) (platform.User, error) {
	return s.ensurer.EnsureUser(ctx, principal)
}

func (s *store) LookupRepository(
	ctx context.Context,
	actor *platform.User,
	owner string,
	slug string,
) (Repository, error) {
	return lookupRepository(ctx, s.pool, actor, owner, slug)
}

func (s *store) RepositoryPermission(
	ctx context.Context,
	actor platform.User,
	repo Repository,
) (Access, error) {
	return repositoryPermission(ctx, s.pool, actor, repo)
}

// permFromRef computes the actor's access from repository and organization IDs
// without requiring a full Repository value. It is a convenience for store
// methods that have only IDs from a join query.
func (s *store) permFromRef(
	ctx context.Context,
	actor platform.User,
	repoID, orgID string,
) (Access, error) {
	var visibility string
	var archived, migrating bool
	err := s.pool.QueryRow(ctx, `
		SELECT visibility, archived_at IS NOT NULL, migrating_at IS NOT NULL
		FROM repositories
		WHERE id = $1 AND organization_id = $2
	`, repoID, orgID).Scan(&visibility, &archived, &migrating)
	if errors.Is(err, pgx.ErrNoRows) {
		return Access{}, platform.ErrNotFound
	}
	if err != nil {
		return Access{}, fmt.Errorf("find repository for permission: %w", err)
	}
	if archived || migrating {
		return Access{}, nil
	}
	return s.RepositoryPermission(ctx, actor, Repository{
		ID: repoID, OrganizationID: orgID, Visibility: visibility,
	})
}

// translateConstraintError maps PostgreSQL duplicate-key violations to
// ErrConflict. Other errors are wrapped with the originating operation.
func translateConstraintError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if isDuplicateKeyError(err) {
		return fmt.Errorf("%s: %w", operation, platform.ErrConflict)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func isDuplicateKeyError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// nowUTC returns the current UTC time used for mutation timestamps.
func nowUTC() time.Time {
	return time.Now().UTC()
}

// insertAudit records an audit event within a transaction.
func insertAudit(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	organizationID string,
	repositoryID string,
	action string,
	targetType string,
	targetID string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			id, organization_id, repository_id, actor_id, action, target_type, target_id
		) VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4, $5, $6, $7)
	`, uuidArg(), organizationID, repositoryID, actorID, action, targetType, targetID)
	if err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}
	return nil
}

// insertOutbox records an outbox event within a transaction.
func insertOutbox(
	ctx context.Context,
	tx pgx.Tx,
	topic string,
	eventKey string,
	payload any,
) error {
	encoded, err := encodeJSON(payload)
	if err != nil {
		return fmt.Errorf("encode outbox payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, $2, $3, $4)
	`, uuidArg(), topic, eventKey, encoded)
	if err != nil {
		return fmt.Errorf("record outbox event: %w", err)
	}
	return nil
}

func uuidArg() string {
	return uuid.NewString()
}

func encodeJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}
