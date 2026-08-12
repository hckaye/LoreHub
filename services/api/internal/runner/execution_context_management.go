package runner

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	executionContextScopeOrganization = "organization"
	executionContextScopeRepository   = "repository"
	executionContextScopeEnvironment  = "environment"
	executionContextValueVariable     = "variable"
	executionContextValueSecret       = "secret"
)

var ErrExecutionContextEntryNotFound = errors.New("Actions execution context entry was not found")

var ErrExecutionContextInvalid = errors.New("Actions execution context input is invalid")

type ExecutionContextScope struct {
	Kind           string `json:"kind"`
	OrganizationID string `json:"organizationId"`
	RepositoryID   string `json:"repositoryId"`
	Environment    string `json:"environment"`
}

type ExecutionContextEntry struct {
	ID        string                `json:"id"`
	Scope     ExecutionContextScope `json:"scope"`
	Name      string                `json:"name"`
	Secret    bool                  `json:"secret"`
	Value     string                `json:"value,omitempty"`
	KeyID     string                `json:"keyId"`
	UpdatedBy string                `json:"updatedBy"`
	CreatedAt time.Time             `json:"createdAt"`
	UpdatedAt time.Time             `json:"updatedAt"`
}

type validatedExecutionContextScope struct {
	kind           string
	organizationID uuid.UUID
	repositoryID   *uuid.UUID
	environment    *string
}

func (resolver *PostgresExecutionContextResolver) UpsertVariable(
	ctx context.Context,
	scope ExecutionContextScope,
	name string,
	value string,
	updatedBy string,
) (ExecutionContextEntry, error) {
	validatedScope, actorID, err := validateExecutionContextMutation(ctx, scope, name, value, updatedBy, false)
	if err != nil {
		return ExecutionContextEntry{}, err
	}
	name = strings.ToUpper(name)
	return resolver.upsertExecutionContextEntry(
		ctx, validatedScope, name, executionContextValueVariable, value, nil, nil, "", actorID,
	)
}

func (resolver *PostgresExecutionContextResolver) UpsertSecret(
	ctx context.Context,
	scope ExecutionContextScope,
	name string,
	value string,
	updatedBy string,
) (ExecutionContextEntry, error) {
	validatedScope, actorID, err := validateExecutionContextMutation(ctx, scope, name, value, updatedBy, true)
	if err != nil {
		return ExecutionContextEntry{}, err
	}
	name = strings.ToUpper(name)
	if resolver == nil || resolver.aead == nil {
		return ExecutionContextEntry{}, errors.New("Actions execution context resolver is not configured")
	}
	nonce := make([]byte, resolver.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return ExecutionContextEntry{}, fmt.Errorf("generate Actions secret nonce: %w", err)
	}
	organizationID, repositoryID, environment := validatedScope.aadValues()
	ciphertext := resolver.aead.Seal(nil, nonce, []byte(value), executionContextAAD(
		validatedScope.kind, organizationID, repositoryID, environment, name,
	))
	return resolver.upsertExecutionContextEntry(
		ctx, validatedScope, name, executionContextValueSecret, "", ciphertext, nonce, resolver.keyID, actorID,
	)
}

func (resolver *PostgresExecutionContextResolver) ListExecutionContextEntries(
	ctx context.Context,
	actorID string,
	scope ExecutionContextScope,
) ([]ExecutionContextEntry, error) {
	if resolver == nil || resolver.pool == nil {
		return nil, errors.New("Actions execution context resolver is not configured")
	}
	validated, err := validateExecutionContextScope(ctx, scope)
	if err != nil {
		return nil, err
	}
	actor, err := uuid.Parse(actorID)
	if err != nil {
		return nil, invalidExecutionContext("actor ID is invalid")
	}
	transaction, err := resolver.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin Actions execution context list: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if err := authorizeExecutionContextManagement(ctx, transaction, validated, actor); err != nil {
		return nil, err
	}
	rows, err := transaction.Query(ctx, `
		SELECT id::text, scope_kind, organization_id::text, repository_id::text,
		       environment_name, value_kind, name, variable_value, key_id,
		       updated_by::text, created_at, updated_at
		FROM actions_execution_context_entries
		WHERE organization_id = $1
		  AND scope_kind = $2
		  AND repository_id IS NOT DISTINCT FROM $3::uuid
		  AND (
			(environment_name IS NULL AND $4::text IS NULL)
			OR lower(environment_name) = lower($4::text)
		  )
		ORDER BY value_kind, lower(name)
	`, validated.organizationID, validated.kind, validated.repositoryArgument(), validated.environmentArgument())
	if err != nil {
		return nil, fmt.Errorf("list Actions execution context entries: %w", err)
	}
	defer rows.Close()
	entries := make([]ExecutionContextEntry, 0)
	for rows.Next() {
		entry, err := scanExecutionContextEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list Actions execution context entries: %w", err)
	}
	rows.Close()
	if err := recordExecutionContextAudit(ctx, transaction, validated, actor,
		"actions.execution_context.list", "", "", map[string]any{"entry_count": len(entries)}); err != nil {
		return nil, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit Actions execution context list: %w", err)
	}
	return entries, nil
}

func (resolver *PostgresExecutionContextResolver) DeleteExecutionContextEntry(
	ctx context.Context,
	actorID string,
	scope ExecutionContextScope,
	name string,
	secret bool,
) error {
	if resolver == nil || resolver.pool == nil {
		return errors.New("Actions execution context resolver is not configured")
	}
	validated, err := validateExecutionContextScope(ctx, scope)
	if err != nil {
		return err
	}
	if err := validateExecutionContextEntryName(name); err != nil {
		return err
	}
	name = strings.ToUpper(name)
	actor, err := uuid.Parse(actorID)
	if err != nil {
		return invalidExecutionContext("actor ID is invalid")
	}
	valueKind := executionContextValueVariable
	if secret {
		valueKind = executionContextValueSecret
	}
	transaction, err := resolver.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin Actions execution context deletion: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if err := authorizeExecutionContextManagement(ctx, transaction, validated, actor); err != nil {
		return err
	}
	command, err := transaction.Exec(ctx, `
		DELETE FROM actions_execution_context_entries
		WHERE organization_id = $1
		  AND scope_kind = $2
		  AND value_kind = $3
		  AND lower(name) = lower($4)
		  AND repository_id IS NOT DISTINCT FROM $5::uuid
		  AND (
			(environment_name IS NULL AND $6::text IS NULL)
			OR lower(environment_name) = lower($6::text)
		  )
	`, validated.organizationID, validated.kind, valueKind, name,
		validated.repositoryArgument(), validated.environmentArgument())
	if err != nil {
		return fmt.Errorf("delete Actions execution context entry: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrExecutionContextEntryNotFound
	}
	if err := recordExecutionContextAudit(ctx, transaction, validated, actor,
		"actions.execution_context.delete", name, valueKind, nil); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Actions execution context deletion: %w", err)
	}
	return nil
}

func (resolver *PostgresExecutionContextResolver) upsertExecutionContextEntry(
	ctx context.Context,
	scope validatedExecutionContextScope,
	name string,
	valueKind string,
	variableValue string,
	encryptedValue []byte,
	nonce []byte,
	keyID string,
	updatedBy uuid.UUID,
) (ExecutionContextEntry, error) {
	if resolver == nil || resolver.pool == nil {
		return ExecutionContextEntry{}, errors.New("Actions execution context resolver is not configured")
	}
	var storedVariable, storedCiphertext, storedNonce, storedKeyID any
	if valueKind == executionContextValueSecret {
		storedCiphertext, storedNonce, storedKeyID = encryptedValue, nonce, keyID
	} else {
		storedVariable = variableValue
	}
	query := `
		INSERT INTO actions_execution_context_entries (
			id, organization_id, repository_id, environment_name, scope_kind,
			value_kind, name, variable_value, encrypted_value, nonce, key_id, updated_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	` + executionContextConflictClause(scope.kind) + `
		DO UPDATE SET
			name = EXCLUDED.name,
			environment_name = EXCLUDED.environment_name,
			variable_value = EXCLUDED.variable_value,
			encrypted_value = EXCLUDED.encrypted_value,
			nonce = EXCLUDED.nonce,
			key_id = EXCLUDED.key_id,
			updated_by = EXCLUDED.updated_by,
			updated_at = now()
		RETURNING id::text, scope_kind, organization_id::text, repository_id::text,
		          environment_name, value_kind, name, variable_value, key_id,
		          updated_by::text, created_at, updated_at
	`
	transaction, err := resolver.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ExecutionContextEntry{}, fmt.Errorf("begin Actions execution context update: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if err := authorizeExecutionContextManagement(ctx, transaction, scope, updatedBy); err != nil {
		return ExecutionContextEntry{}, err
	}
	row := transaction.QueryRow(ctx, query,
		uuid.New(), scope.organizationID, scope.repositoryArgument(), scope.environmentArgument(),
		scope.kind, valueKind, name, storedVariable, storedCiphertext, storedNonce, storedKeyID, updatedBy,
	)
	entry, err := scanExecutionContextEntry(row)
	if err != nil {
		return ExecutionContextEntry{}, fmt.Errorf("upsert Actions execution context entry: %w", err)
	}
	auditDetails := make(map[string]any)
	if entry.KeyID != "" {
		auditDetails["key_id"] = entry.KeyID
	}
	if err := recordExecutionContextAudit(ctx, transaction, scope, updatedBy,
		"actions.execution_context.upsert", name, valueKind, auditDetails); err != nil {
		return ExecutionContextEntry{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return ExecutionContextEntry{}, fmt.Errorf("commit Actions execution context update: %w", err)
	}
	return entry, nil
}

func executionContextConflictClause(scopeKind string) string {
	switch scopeKind {
	case executionContextScopeOrganization:
		return `
			ON CONFLICT (organization_id, value_kind, lower(name))
			WHERE scope_kind = 'organization'
		`
	case executionContextScopeRepository:
		return `
			ON CONFLICT (repository_id, value_kind, lower(name))
			WHERE scope_kind = 'repository'
		`
	case executionContextScopeEnvironment:
		return `
			ON CONFLICT (repository_id, lower(environment_name), value_kind, lower(name))
			WHERE scope_kind = 'environment'
		`
	default:
		panic("validated Actions execution context scope is invalid")
	}
}

func scanExecutionContextEntry(row pgx.Row) (ExecutionContextEntry, error) {
	var entry ExecutionContextEntry
	var scopeKind, valueKind, organizationID string
	var repositoryID, environment, keyID *string
	var variableValue *string
	if err := row.Scan(
		&entry.ID, &scopeKind, &organizationID, &repositoryID, &environment,
		&valueKind, &entry.Name, &variableValue, &keyID, &entry.UpdatedBy,
		&entry.CreatedAt, &entry.UpdatedAt,
	); err != nil {
		return ExecutionContextEntry{}, err
	}
	entry.Scope = ExecutionContextScope{Kind: scopeKind, OrganizationID: organizationID}
	if repositoryID != nil {
		entry.Scope.RepositoryID = *repositoryID
	}
	if environment != nil {
		entry.Scope.Environment = *environment
	}
	entry.Secret = valueKind == executionContextValueSecret
	if !entry.Secret && variableValue != nil {
		entry.Value = *variableValue
	}
	if entry.Secret {
		entry.Value = ""
	}
	if keyID != nil {
		entry.KeyID = *keyID
	}
	return entry, nil
}

func authorizeExecutionContextManagement(
	ctx context.Context,
	transaction pgx.Tx,
	scope validatedExecutionContextScope,
	actorID uuid.UUID,
) error {
	var authorized bool
	var err error
	if scope.kind == executionContextScopeOrganization {
		err = transaction.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM users actor
				JOIN organization_memberships membership
				  ON membership.user_id = actor.id
				 AND membership.organization_id = $2
				 AND membership.active
				JOIN organizations organization
				  ON organization.id = membership.organization_id
				 AND organization.active
				WHERE actor.id = $1
				  AND actor.status = 'active'
				  AND membership.role IN ('owner', 'maintainer')
			)
		`, actorID, scope.organizationID).Scan(&authorized)
	} else {
		err = transaction.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM users actor
				JOIN repositories repository
				  ON repository.id = $2
				 AND repository.organization_id = $3
				 AND repository.lifecycle_state = 'active'
				 AND repository.archived_at IS NULL
				JOIN organizations organization
				  ON organization.id = repository.organization_id
				 AND organization.active
				WHERE actor.id = $1
				  AND actor.status = 'active'
				  AND (
				    $4::text IS NULL
				    OR EXISTS (
				      SELECT 1 FROM repository_environments environment
				      WHERE environment.repository_id = repository.id
				        AND lower(environment.name) = lower($4::text)
				        AND environment.active
				    )
				  )
				  AND (
					EXISTS (
						SELECT 1
						FROM organization_memberships membership
						WHERE membership.organization_id = organization.id
						  AND membership.user_id = actor.id
						  AND membership.active
						  AND membership.role = 'owner'
					)
					OR EXISTS (
						SELECT 1
						FROM repository_memberships membership
						WHERE membership.repository_id = repository.id
						  AND membership.user_id = actor.id
						  AND membership.active
						  AND membership.role = 'admin'
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
						 AND team_membership.user_id = actor.id
						 AND team_membership.active
						JOIN organization_memberships organization_membership
						  ON organization_membership.organization_id = organization.id
						 AND organization_membership.user_id = actor.id
						 AND organization_membership.active
						WHERE role.repository_id = repository.id
						  AND role.active
						  AND role.role = 'admin'
					)
				  )
			)
		`, actorID, scope.repositoryArgument(), scope.organizationID, scope.environmentArgument()).Scan(&authorized)
	}
	if err != nil {
		return fmt.Errorf("authorize Actions execution context management: %w", err)
	}
	if !authorized {
		return ErrExecutionContextUnauthorized
	}
	return nil
}

func recordExecutionContextAudit(
	ctx context.Context,
	transaction pgx.Tx,
	scope validatedExecutionContextScope,
	actorID uuid.UUID,
	action string,
	name string,
	valueKind string,
	details map[string]any,
) error {
	if details == nil {
		details = make(map[string]any)
	}
	details["scope_kind"] = scope.kind
	if name != "" {
		details["name"] = name
	}
	if valueKind != "" {
		details["value_kind"] = valueKind
	}
	if scope.environment != nil {
		details["environment"] = *scope.environment
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode Actions execution context audit: %w", err)
	}
	targetID := scope.organizationID.String()
	if scope.repositoryID != nil {
		targetID = scope.repositoryID.String()
	}
	if scope.environment != nil {
		targetID += ":" + *scope.environment
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO audit_events (
			id, organization_id, repository_id, actor_id,
			action, target_type, target_id, details
		) VALUES ($1, $2, $3, $4, $5, 'actions_execution_context', $6, $7)
	`, uuid.New(), scope.organizationID, scope.repositoryArgument(), actorID, action, targetID, encoded)
	if err != nil {
		return fmt.Errorf("record Actions execution context audit: %w", err)
	}
	return nil
}

func validateExecutionContextMutation(
	ctx context.Context,
	scope ExecutionContextScope,
	name string,
	value string,
	updatedBy string,
	secret bool,
) (validatedExecutionContextScope, uuid.UUID, error) {
	validatedScope, err := validateExecutionContextScope(ctx, scope)
	if err != nil {
		return validatedExecutionContextScope{}, uuid.Nil, err
	}
	if err := validateExecutionContextEntryName(name); err != nil {
		return validatedExecutionContextScope{}, uuid.Nil, err
	}
	if err := validateExecutionValues(map[string]string{name: value}, secret); err != nil {
		return validatedExecutionContextScope{}, uuid.Nil, fmt.Errorf("%w: %v", ErrExecutionContextInvalid, err)
	}
	actorID, err := uuid.Parse(updatedBy)
	if err != nil {
		return validatedExecutionContextScope{}, uuid.Nil, invalidExecutionContext("actor ID is invalid")
	}
	return validatedScope, actorID, nil
}

func validateExecutionContextEntryName(name string) error {
	if !executionNamePattern.MatchString(name) || isReservedExecutionName(name) {
		return invalidExecutionContext("name %q is invalid or reserved", name)
	}
	return nil
}

func validateExecutionContextScope(
	ctx context.Context,
	scope ExecutionContextScope,
) (validatedExecutionContextScope, error) {
	if err := ctx.Err(); err != nil {
		return validatedExecutionContextScope{}, err
	}
	organizationID, err := uuid.Parse(scope.OrganizationID)
	if err != nil {
		return validatedExecutionContextScope{}, invalidExecutionContext("organization ID is invalid")
	}
	validated := validatedExecutionContextScope{kind: scope.Kind, organizationID: organizationID}
	switch scope.Kind {
	case executionContextScopeOrganization:
		if scope.RepositoryID != "" || scope.Environment != "" {
			return validatedExecutionContextScope{}, invalidExecutionContext("organization scope is inconsistent")
		}
	case executionContextScopeRepository:
		if scope.Environment != "" {
			return validatedExecutionContextScope{}, invalidExecutionContext("repository scope is inconsistent")
		}
		if err := validated.setRepositoryID(scope.RepositoryID); err != nil {
			return validatedExecutionContextScope{}, err
		}
	case executionContextScopeEnvironment:
		if err := validated.setRepositoryID(scope.RepositoryID); err != nil {
			return validatedExecutionContextScope{}, err
		}
		if scope.Environment == "" || scope.Environment != strings.TrimSpace(scope.Environment) ||
			len(scope.Environment) > 128 || hasControlCharacter(scope.Environment) {
			return validatedExecutionContextScope{}, invalidExecutionContext("environment scope is invalid")
		}
		validated.environment = &scope.Environment
	default:
		return validatedExecutionContextScope{}, invalidExecutionContext("scope kind is invalid")
	}
	return validated, nil
}

func (scope *validatedExecutionContextScope) setRepositoryID(value string) error {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return invalidExecutionContext("repository ID is invalid")
	}
	scope.repositoryID = &parsed
	return nil
}

func (scope validatedExecutionContextScope) repositoryArgument() any {
	if scope.repositoryID == nil {
		return nil
	}
	return *scope.repositoryID
}

func (scope validatedExecutionContextScope) environmentArgument() any {
	if scope.environment == nil {
		return nil
	}
	return *scope.environment
}

func (scope validatedExecutionContextScope) aadValues() (string, string, string) {
	repositoryID := ""
	if scope.repositoryID != nil {
		repositoryID = scope.repositoryID.String()
	}
	environment := ""
	if scope.environment != nil {
		environment = *scope.environment
	}
	return scope.organizationID.String(), repositoryID, environment
}

func hasControlCharacter(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func invalidExecutionContext(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", ErrExecutionContextInvalid, fmt.Sprintf(format, arguments...))
}
