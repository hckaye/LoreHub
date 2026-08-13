package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/auth"
)

const (
	runnerNameLimit            = 100
	runnerLabelLimit           = 100
	runnerLabelLengthLimit     = 100
	runnerVersionLimit         = 64
	runnerRegistrationTokenAge = 24 * time.Hour
	runnerCredentialMaxAge     = 366 * 24 * time.Hour
)

var runnerCredentialKeyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type RunnerScope struct {
	OrganizationID string `json:"organizationId,omitempty"`
	RepositoryID   string `json:"repositoryId,omitempty"`
	UserID         string `json:"userId,omitempty"`
}

type Runner struct {
	ID                  string      `json:"id"`
	Scope               RunnerScope `json:"scope"`
	Name                string      `json:"name"`
	Labels              []string    `json:"labels"`
	CredentialExpiresAt time.Time   `json:"credentialExpiresAt"`
	LastUsedAt          *time.Time  `json:"lastUsedAt"`
	RevokedAt           *time.Time  `json:"revokedAt"`
	RunnerVersion       string      `json:"runnerVersion"`
	LastSeenAt          *time.Time  `json:"lastSeenAt"`
	CreatedAt           time.Time   `json:"createdAt"`
}

type RunnerRegistrationToken struct {
	ID         string
	Scope      RunnerScope
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedBy  string
	CreatedAt  time.Time
}

type CreateRunnerRegistrationTokenInput struct {
	Scope     RunnerScope
	Digest    []byte
	ExpiresAt time.Time
}

type RegisterRunnerInput struct {
	RegistrationTokenID string
	Name                string
	Labels              []string
	CredentialDigest    []byte
	CredentialKeyID     string
	CredentialExpiresAt time.Time
	RunnerVersion       string
}

func (store *Store) CreateRegistrationToken(
	ctx context.Context,
	actor User,
	input CreateRunnerRegistrationTokenInput,
) (RunnerRegistrationToken, error) {
	scope, ok := normalizeRunnerScope(input.Scope)
	if !ok || scope.UserID != "" || len(input.Digest) != 32 || input.ExpiresAt.IsZero() {
		return RunnerRegistrationToken{}, ErrInvalidInput
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return RunnerRegistrationToken{}, fmt.Errorf("begin runner registration token creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if err := authorizeRunnerScopeManagement(ctx, transaction, actor.ID, scope); err != nil {
		return RunnerRegistrationToken{}, err
	}
	var createdAt time.Time
	if err := transaction.QueryRow(ctx, "SELECT now()").Scan(&createdAt); err != nil {
		return RunnerRegistrationToken{}, fmt.Errorf("read runner registration token creation time: %w", err)
	}
	expiresAt := input.ExpiresAt.UTC()
	if !expiresAt.After(createdAt) || expiresAt.After(createdAt.Add(runnerRegistrationTokenAge)) {
		return RunnerRegistrationToken{}, ErrInvalidInput
	}
	token := RunnerRegistrationToken{
		ID:        uuid.NewString(),
		Scope:     scope,
		ExpiresAt: expiresAt,
		CreatedBy: actor.ID,
		CreatedAt: createdAt.UTC(),
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO runner_registration_tokens (
			id, organization_id, repository_id, user_id,
			token_digest, expires_at, created_by, created_at
		) VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid,
		          NULLIF($4, '')::uuid, $5, $6, $7, $8)
	`, token.ID, scope.OrganizationID, scope.RepositoryID, scope.UserID,
		input.Digest, token.ExpiresAt, actor.ID, token.CreatedAt)
	if err != nil {
		return RunnerRegistrationToken{}, translateRunnerConstraintError(
			"create runner registration token", err,
		)
	}
	if err := insertAudit(ctx, transaction, actor.ID, scope.OrganizationID, scope.RepositoryID,
		"runner.registration_token.create", "runner_registration_token", token.ID); err != nil {
		return RunnerRegistrationToken{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return RunnerRegistrationToken{}, fmt.Errorf("commit runner registration token creation: %w", err)
	}
	return token, nil
}

func (store *Store) ConsumeRegistrationToken(
	ctx context.Context,
	digest []byte,
) (RunnerRegistrationToken, error) {
	if len(digest) != 32 {
		return RunnerRegistrationToken{}, auth.ErrInvalidRunnerToken
	}
	var token RunnerRegistrationToken
	var organizationID, repositoryID, userID *string
	err := store.pool.QueryRow(ctx, `
		UPDATE runner_registration_tokens
		SET consumed_at = now()
		WHERE token_digest = $1
		  AND consumed_at IS NULL
		  AND expires_at > now()
		RETURNING id, organization_id, repository_id, user_id,
		          expires_at, consumed_at, created_by, created_at
	`, digest).Scan(
		&token.ID, &organizationID, &repositoryID, &userID,
		&token.ExpiresAt, &token.ConsumedAt, &token.CreatedBy, &token.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RunnerRegistrationToken{}, auth.ErrInvalidRunnerToken
	}
	if err != nil {
		return RunnerRegistrationToken{}, fmt.Errorf("consume runner registration token: %w", err)
	}
	token.Scope = runnerScopeFromPointers(organizationID, repositoryID, userID)
	if _, ok := normalizeRunnerScope(token.Scope); !ok || token.Scope.UserID != "" {
		return RunnerRegistrationToken{}, auth.ErrInvalidRunnerToken
	}
	return token, nil
}

func (store *Store) RegisterRunner(
	ctx context.Context,
	input RegisterRunnerInput,
) (Runner, error) {
	registrationTokenID, err := uuid.Parse(strings.TrimSpace(input.RegistrationTokenID))
	if err != nil {
		return Runner{}, ErrInvalidInput
	}
	name := strings.TrimSpace(input.Name)
	version := strings.TrimSpace(input.RunnerVersion)
	labels, ok := normalizeRunnerLabels(input.Labels)
	if !ok || !validRunnerText(name, runnerNameLimit) || !validRunnerText(version, runnerVersionLimit) ||
		len(input.CredentialDigest) != 32 ||
		!runnerCredentialKeyIDPattern.MatchString(input.CredentialKeyID) ||
		input.CredentialExpiresAt.IsZero() {
		return Runner{}, ErrInvalidInput
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Runner{}, fmt.Errorf("begin runner registration: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var scope RunnerScope
	var organizationID, repositoryID, userID *string
	var createdBy string
	var createdAt time.Time
	err = transaction.QueryRow(ctx, `
		SELECT organization_id, repository_id, user_id, created_by, now()
		FROM runner_registration_tokens
		WHERE id = $1 AND consumed_at IS NOT NULL
		FOR UPDATE
	`, registrationTokenID).Scan(&organizationID, &repositoryID, &userID, &createdBy, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Runner{}, auth.ErrInvalidRunnerToken
	}
	if err != nil {
		return Runner{}, fmt.Errorf("load consumed runner registration token: %w", err)
	}
	scope = runnerScopeFromPointers(organizationID, repositoryID, userID)
	if normalized, valid := normalizeRunnerScope(scope); !valid || normalized.UserID != "" {
		return Runner{}, auth.ErrInvalidRunnerToken
	} else {
		scope = normalized
	}
	expiresAt := input.CredentialExpiresAt.UTC()
	if !expiresAt.After(createdAt) || expiresAt.After(createdAt.Add(runnerCredentialMaxAge)) {
		return Runner{}, ErrInvalidInput
	}
	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		return Runner{}, fmt.Errorf("encode runner labels: %w", err)
	}
	runner := Runner{
		ID:                  uuid.NewString(),
		Scope:               scope,
		Name:                name,
		Labels:              labels,
		CredentialExpiresAt: expiresAt,
		RunnerVersion:       version,
		CreatedAt:           createdAt.UTC(),
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO ci_runners (
			id, organization_id, repository_id, user_id, name, labels,
			credential_digest, credential_key_id, credential_expires_at,
			runner_version, created_at
		) VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid,
		          NULLIF($4, '')::uuid, $5, $6, $7, $8, $9, $10, $11)
	`, runner.ID, scope.OrganizationID, scope.RepositoryID, scope.UserID, runner.Name, labelsJSON,
		input.CredentialDigest, input.CredentialKeyID, runner.CredentialExpiresAt,
		runner.RunnerVersion, runner.CreatedAt)
	if err != nil {
		return Runner{}, translateRunnerConstraintError("register runner", err)
	}
	if err := insertAudit(ctx, transaction, createdBy, scope.OrganizationID, scope.RepositoryID,
		"runner.register", "ci_runner", runner.ID); err != nil {
		return Runner{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return Runner{}, fmt.Errorf("commit runner registration: %w", err)
	}
	return runner, nil
}

func (store *Store) ListRunners(
	ctx context.Context,
	actor User,
	scope RunnerScope,
) ([]Runner, error) {
	normalizedScope, ok := normalizeRunnerScope(scope)
	if !ok || normalizedScope.UserID != "" {
		return nil, ErrInvalidInput
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin runner list: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if err := authorizeRunnerScopeManagement(ctx, transaction, actor.ID, normalizedScope); err != nil {
		return nil, err
	}
	rows, err := transaction.Query(ctx, `
		SELECT id, organization_id, repository_id, user_id, name, labels,
		       credential_expires_at, last_used_at, revoked_at,
		       runner_version, last_seen_at, created_at
		FROM ci_runners
		WHERE organization_id IS NOT DISTINCT FROM NULLIF($1, '')::uuid
		  AND repository_id IS NOT DISTINCT FROM NULLIF($2, '')::uuid
		  AND user_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
		ORDER BY created_at DESC, id DESC
	`, normalizedScope.OrganizationID, normalizedScope.RepositoryID, normalizedScope.UserID)
	if err != nil {
		return nil, fmt.Errorf("list runners: %w", err)
	}
	defer rows.Close()
	runners := make([]Runner, 0)
	for rows.Next() {
		runner, err := scanRunner(rows)
		if err != nil {
			return nil, fmt.Errorf("scan runner: %w", err)
		}
		runners = append(runners, runner)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runners: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit runner list: %w", err)
	}
	return runners, nil
}

func (store *Store) RevokeRunner(
	ctx context.Context,
	actor User,
	scope RunnerScope,
	runnerID string,
) error {
	normalizedScope, ok := normalizeRunnerScope(scope)
	parsedRunnerID, idErr := uuid.Parse(strings.TrimSpace(runnerID))
	if !ok || normalizedScope.UserID != "" || idErr != nil {
		return ErrNotFound
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin runner revocation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if err := authorizeRunnerScopeManagement(ctx, transaction, actor.ID, normalizedScope); err != nil {
		return err
	}
	command, err := transaction.Exec(ctx, `
		UPDATE ci_runners
		SET revoked_at = now()
		WHERE id = $1
		  AND organization_id IS NOT DISTINCT FROM NULLIF($2, '')::uuid
		  AND repository_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
		  AND user_id IS NOT DISTINCT FROM NULLIF($4, '')::uuid
		  AND revoked_at IS NULL
	`, parsedRunnerID, normalizedScope.OrganizationID,
		normalizedScope.RepositoryID, normalizedScope.UserID)
	if err != nil {
		return fmt.Errorf("revoke runner: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrNotFound
	}
	if err := insertAudit(ctx, transaction, actor.ID, normalizedScope.OrganizationID,
		normalizedScope.RepositoryID, "runner.revoke", "ci_runner", parsedRunnerID.String()); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit runner revocation: %w", err)
	}
	return nil
}

func (store *Store) TouchRunnerSeen(ctx context.Context, runnerID string, seenAt time.Time) error {
	parsedRunnerID, err := uuid.Parse(strings.TrimSpace(runnerID))
	if err != nil || seenAt.IsZero() {
		return auth.ErrInvalidRunnerToken
	}
	command, err := store.pool.Exec(ctx, `
		UPDATE ci_runners
		SET last_seen_at = GREATEST(COALESCE(last_seen_at, $2), $2)
		WHERE id = $1
		  AND revoked_at IS NULL
		  AND credential_expires_at > $2
		  AND created_at <= $2
	`, parsedRunnerID, seenAt.UTC())
	if err != nil {
		return fmt.Errorf("record runner heartbeat: %w", err)
	}
	if command.RowsAffected() != 1 {
		return auth.ErrInvalidRunnerToken
	}
	return nil
}

func (store *Store) AuthenticateRunner(
	ctx context.Context,
	digest []byte,
	credentialKeyID string,
	usedAt time.Time,
) (Runner, error) {
	if len(digest) != 32 || !runnerCredentialKeyIDPattern.MatchString(credentialKeyID) || usedAt.IsZero() {
		return Runner{}, auth.ErrInvalidRunnerToken
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Runner{}, fmt.Errorf("begin runner authentication: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	runner, err := scanRunner(transaction.QueryRow(ctx, `
		SELECT runner.id, runner.organization_id, runner.repository_id, runner.user_id,
		       runner.name, runner.labels, runner.credential_expires_at,
		       runner.last_used_at, runner.revoked_at, runner.runner_version,
		       runner.last_seen_at, runner.created_at
		FROM ci_runners runner
		WHERE runner.credential_digest = $1
		  AND runner.credential_key_id = $2
		  AND runner.revoked_at IS NULL
		  AND runner.credential_expires_at > $3
		  AND runner.user_id IS NULL
		  AND EXISTS (
		      SELECT 1 FROM organizations organization
		      WHERE organization.id = runner.organization_id AND organization.active
		  )
		  AND (
		      runner.repository_id IS NULL
		      OR EXISTS (
		          SELECT 1 FROM repositories repository
		          WHERE repository.id = runner.repository_id
		            AND repository.organization_id = runner.organization_id
		            AND repository.lifecycle_state = 'active'
		            AND repository.archived_at IS NULL AND repository.migrating_at IS NULL
		      )
		  )
		FOR UPDATE
	`, digest, credentialKeyID, usedAt.UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return Runner{}, auth.ErrInvalidRunnerToken
	}
	if err != nil {
		return Runner{}, fmt.Errorf("authenticate runner: %w", err)
	}
	if runner.LastUsedAt == nil || runner.LastUsedAt.Before(usedAt.Add(-5*time.Minute)) {
		if _, err := transaction.Exec(ctx, `
			UPDATE ci_runners SET last_used_at = $2 WHERE id = $1
		`, runner.ID, usedAt.UTC()); err != nil {
			return Runner{}, fmt.Errorf("record runner credential use: %w", err)
		}
		recorded := usedAt.UTC()
		runner.LastUsedAt = &recorded
	}
	if err := transaction.Commit(ctx); err != nil {
		return Runner{}, fmt.Errorf("commit runner authentication: %w", err)
	}
	return runner, nil
}

func authorizeRunnerScopeManagement(
	ctx context.Context,
	transaction pgx.Tx,
	actorID string,
	scope RunnerScope,
) error {
	actor, err := uuid.Parse(strings.TrimSpace(actorID))
	if err != nil {
		return ErrForbidden
	}
	var scopeExists bool
	if scope.RepositoryID == "" {
		err = transaction.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM organizations WHERE id = $1 AND active
			)
		`, scope.OrganizationID).Scan(&scopeExists)
	} else {
		err = transaction.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM repositories repository
				JOIN organizations organization
				  ON organization.id = repository.organization_id AND organization.active
				WHERE repository.id = $1 AND repository.organization_id = $2
				  AND repository.lifecycle_state = 'active'
				  AND repository.archived_at IS NULL AND repository.migrating_at IS NULL
			)
		`, scope.RepositoryID, scope.OrganizationID).Scan(&scopeExists)
	}
	if err != nil {
		return fmt.Errorf("find runner scope: %w", err)
	}
	if !scopeExists {
		return ErrNotFound
	}
	var authorized bool
	if scope.RepositoryID == "" {
		err = transaction.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM users actor
				JOIN organization_memberships membership
				  ON membership.user_id = actor.id
				 AND membership.organization_id = $2
				 AND membership.active
				WHERE actor.id = $1 AND actor.status = 'active'
				  AND membership.role IN ('owner', 'maintainer')
			)
		`, actor, scope.OrganizationID).Scan(&authorized)
	} else {
		err = transaction.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM users actor
				WHERE actor.id = $1 AND actor.status = 'active'
				  AND (
				    EXISTS (
				      SELECT 1 FROM organization_memberships membership
				      WHERE membership.organization_id = $2
				        AND membership.user_id = actor.id
				        AND membership.active AND membership.role = 'owner'
				    )
				    OR EXISTS (
				      SELECT 1 FROM repository_memberships membership
				      WHERE membership.repository_id = $3
				        AND membership.user_id = actor.id
				        AND membership.active AND membership.role = 'admin'
				    )
				    OR EXISTS (
				      SELECT 1
				      FROM team_repository_roles role
				      JOIN teams team
				        ON team.id = role.team_id
				       AND team.organization_id = $2 AND team.active
				      JOIN team_memberships team_membership
				        ON team_membership.team_id = team.id
				       AND team_membership.user_id = actor.id AND team_membership.active
				      JOIN organization_memberships organization_membership
				        ON organization_membership.organization_id = $2
				       AND organization_membership.user_id = actor.id
				       AND organization_membership.active
				      WHERE role.repository_id = $3 AND role.active AND role.role = 'admin'
				    )
				  )
			)
		`, actor, scope.OrganizationID, scope.RepositoryID).Scan(&authorized)
	}
	if err != nil {
		return fmt.Errorf("authorize runner management: %w", err)
	}
	if !authorized {
		return ErrForbidden
	}
	return nil
}

func normalizeRunnerScope(scope RunnerScope) (RunnerScope, bool) {
	values := []*string{&scope.OrganizationID, &scope.RepositoryID, &scope.UserID}
	for _, value := range values {
		*value = strings.TrimSpace(*value)
		if *value == "" {
			continue
		}
		parsed, err := uuid.Parse(*value)
		if err != nil {
			return RunnerScope{}, false
		}
		*value = parsed.String()
	}
	valid := (scope.OrganizationID != "" && scope.RepositoryID != "" && scope.UserID == "") ||
		(scope.OrganizationID != "" && scope.RepositoryID == "" && scope.UserID == "") ||
		(scope.OrganizationID == "" && scope.RepositoryID == "" && scope.UserID != "")
	return scope, valid
}

func normalizeRunnerLabels(labels []string) ([]string, bool) {
	if len(labels) == 0 || len(labels) > runnerLabelLimit {
		return nil, false
	}
	seen := make(map[string]struct{}, len(labels))
	result := make([]string, 0, len(labels))
	for _, label := range labels {
		label = strings.ToLower(strings.TrimSpace(label))
		if !validRunnerText(label, runnerLabelLengthLimit) {
			return nil, false
		}
		if _, exists := seen[label]; exists {
			continue
		}
		seen[label] = struct{}{}
		result = append(result, label)
	}
	if len(result) == 0 {
		return nil, false
	}
	sort.Strings(result)
	return result, true
}

func validRunnerText(value string, limit int) bool {
	if value == "" || utf8.RuneCountInString(value) > limit {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func runnerScopeFromPointers(organizationID, repositoryID, userID *string) RunnerScope {
	var scope RunnerScope
	if organizationID != nil {
		scope.OrganizationID = *organizationID
	}
	if repositoryID != nil {
		scope.RepositoryID = *repositoryID
	}
	if userID != nil {
		scope.UserID = *userID
	}
	return scope
}

func scanRunner(row rowScanner) (Runner, error) {
	var runner Runner
	var organizationID, repositoryID, userID *string
	var labelsJSON []byte
	err := row.Scan(
		&runner.ID, &organizationID, &repositoryID, &userID, &runner.Name, &labelsJSON,
		&runner.CredentialExpiresAt, &runner.LastUsedAt, &runner.RevokedAt,
		&runner.RunnerVersion, &runner.LastSeenAt, &runner.CreatedAt,
	)
	if err != nil {
		return Runner{}, err
	}
	if err := json.Unmarshal(labelsJSON, &runner.Labels); err != nil {
		return Runner{}, fmt.Errorf("decode runner labels: %w", err)
	}
	runner.Scope = runnerScopeFromPointers(organizationID, repositoryID, userID)
	return runner, nil
}

func translateRunnerConstraintError(operation string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", operation, ErrNotFound)
	}
	return translateConstraintError(operation, err)
}
