package runner

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrExecutionContextUnauthorized = errors.New("Actions execution context access is not authorized")

var executionContextKeyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type PostgresExecutionContextResolver struct {
	pool  *pgxpool.Pool
	aead  cipher.AEAD
	keyID string
}

// NewPostgresExecutionContextResolver creates the fail-closed production resolver.
func NewPostgresExecutionContextResolver(
	pool *pgxpool.Pool,
	keyID string,
	encodedKey string,
) (*PostgresExecutionContextResolver, error) {
	if pool == nil {
		return nil, errors.New("Actions execution context PostgreSQL pool is required")
	}
	if !executionContextKeyIDPattern.MatchString(keyID) {
		return nil, errors.New("Actions execution context encryption key ID is invalid")
	}
	if strings.TrimSpace(encodedKey) != encodedKey || encodedKey == "" || strings.ContainsAny(encodedKey, "\r\n") {
		return nil, errors.New("Actions execution context encryption key must be base64 encoded")
	}
	key, err := base64.StdEncoding.Strict().DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		clear(key)
		return nil, errors.New("Actions execution context encryption key must decode to 32 bytes")
	}
	block, err := aes.NewCipher(key)
	clear(key)
	if err != nil {
		return nil, fmt.Errorf("create Actions execution context cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create Actions execution context GCM: %w", err)
	}
	return &PostgresExecutionContextResolver{pool: pool, aead: aead, keyID: keyID}, nil
}

func (resolver *PostgresExecutionContextResolver) Resolve(
	ctx context.Context,
	request ExecutionContextRequest,
) (ExecutionContext, error) {
	if resolver == nil || resolver.pool == nil || resolver.aead == nil {
		return ExecutionContext{}, errors.New("Actions execution context resolver is not configured")
	}
	validated, err := validateProductionExecutionContextRequest(ctx, request)
	if err != nil {
		return ExecutionContext{}, err
	}
	transaction, err := resolver.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return ExecutionContext{}, fmt.Errorf("begin Actions execution context transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if err := authorizeExecutionContext(ctx, transaction, validated); err != nil {
		return ExecutionContext{}, err
	}
	resolved, err := resolver.loadExecutionContext(ctx, transaction, validated)
	if err != nil {
		return ExecutionContext{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return ExecutionContext{}, fmt.Errorf("commit Actions execution context read: %w", err)
	}
	if err := validateExecutionContext(resolved); err != nil {
		return ExecutionContext{}, fmt.Errorf("validate stored Actions execution context: %w", err)
	}
	return resolved, nil
}

type validatedExecutionContextRequest struct {
	principalSubject string
	repositoryID     uuid.UUID
	organizationID   uuid.UUID
	jobID            uuid.UUID
	environment      string
}

func validateProductionExecutionContextRequest(
	ctx context.Context,
	request ExecutionContextRequest,
) (validatedExecutionContextRequest, error) {
	if err := validateExecutionContextRequest(ctx, request); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return validatedExecutionContextRequest{}, err
		}
		return validatedExecutionContextRequest{}, fmt.Errorf("%w: %v", ErrExecutionContextInvalid, err)
	}
	if request.Principal.Kind != "service" {
		return validatedExecutionContextRequest{}, ErrExecutionContextUnauthorized
	}
	subject := strings.TrimSpace(request.Principal.Subject)
	if subject != request.Principal.Subject || len(subject) > 128 || strings.ContainsAny(subject, "\x00\r\n") {
		return validatedExecutionContextRequest{}, invalidExecutionContext("principal subject is invalid")
	}
	repositoryID, err := uuid.Parse(request.RepositoryID)
	if err != nil {
		return validatedExecutionContextRequest{}, invalidExecutionContext("repository ID is invalid")
	}
	organizationID, err := uuid.Parse(request.OrganizationID)
	if err != nil {
		return validatedExecutionContextRequest{}, invalidExecutionContext("organization ID is invalid")
	}
	if request.Environment != strings.TrimSpace(request.Environment) || hasControlCharacter(request.Environment) {
		return validatedExecutionContextRequest{}, invalidExecutionContext("environment is invalid")
	}
	jobID, err := uuid.Parse(request.JobID)
	if err != nil {
		return validatedExecutionContextRequest{}, invalidExecutionContext("job ID is invalid")
	}
	return validatedExecutionContextRequest{
		principalSubject: subject,
		repositoryID:     repositoryID,
		organizationID:   organizationID,
		jobID:            jobID,
		environment:      request.Environment,
	}, nil
}

func authorizeExecutionContext(
	ctx context.Context,
	transaction pgx.Tx,
	request validatedExecutionContextRequest,
) error {
	var authorized bool
	err := transaction.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM service_principals principal
			JOIN service_principal_repository_grants service_grant
			  ON service_grant.principal_id = principal.id
			 AND service_grant.repository_id = $1
			 AND service_grant.active
			JOIN repositories repository
			  ON repository.id = service_grant.repository_id
			 AND repository.organization_id = $2
			 AND repository.lifecycle_state = 'active'
			 AND repository.archived_at IS NULL AND repository.migrating_at IS NULL
			JOIN organizations organization
			  ON organization.id = repository.organization_id
			 AND organization.active
			WHERE principal.active
			  AND principal.kind = 'ci_runner'
			  AND (principal.name = $3 OR principal.id::text = $3)
			  AND 'read' = ANY(service_grant.permissions)
			  AND EXISTS (
			    SELECT 1
			    FROM ci_jobs job
			    JOIN ci_runs run
			      ON run.id = job.run_id
			     AND run.repository_id = repository.id
			     AND run.status = 'in_progress'
			     AND NOT run.cancel_requested
			    LEFT JOIN deployments deployment ON deployment.job_id = job.id
			    WHERE job.id = $5
			      AND job.status = 'in_progress'
			      AND job.lease_expires_at > now()
			      AND (
			        ($4 = '' AND deployment.id IS NULL)
			        OR (
			          $4 <> ''
			          AND lower(deployment.environment_name) = lower($4)
			          AND deployment.status = 'in_progress'
			        )
			      )
			  )
			  AND (
			    $4 = ''
			    OR EXISTS (
			      SELECT 1 FROM repository_environments environment
			      WHERE environment.repository_id = repository.id
			        AND lower(environment.name) = lower($4)
			        AND environment.active
			    )
			  )
		)
	`, request.repositoryID, request.organizationID, request.principalSubject, request.environment,
		request.jobID).Scan(&authorized)
	if err != nil {
		return fmt.Errorf("authorize Actions execution context: %w", err)
	}
	if !authorized {
		return ErrExecutionContextUnauthorized
	}
	return nil
}

func (resolver *PostgresExecutionContextResolver) loadExecutionContext(
	ctx context.Context,
	transaction pgx.Tx,
	request validatedExecutionContextRequest,
) (ExecutionContext, error) {
	rows, err := transaction.Query(ctx, `
		SELECT scope_kind, value_kind, name, variable_value, encrypted_value, nonce, key_id,
		       organization_id::text, COALESCE(repository_id::text, ''),
		       COALESCE(environment_name, '')
		FROM actions_execution_context_entries
		WHERE organization_id = $1
		  AND (
			scope_kind = 'organization'
			OR (scope_kind = 'repository' AND repository_id = $2)
			OR (
				scope_kind = 'environment'
				AND repository_id = $2
				AND $3 <> ''
				AND lower(environment_name) = lower($3)
			)
		  )
		ORDER BY scope_kind, value_kind, name
	`, request.organizationID, request.repositoryID, request.environment)
	if err != nil {
		return ExecutionContext{}, fmt.Errorf("read Actions execution context: %w", err)
	}
	defer rows.Close()
	result := emptyExecutionContext()
	for rows.Next() {
		if err := resolver.scanExecutionContextRow(rows, &result); err != nil {
			return ExecutionContext{}, err
		}
	}
	if err := rows.Err(); err != nil {
		return ExecutionContext{}, fmt.Errorf("read Actions execution context: %w", err)
	}
	return result, nil
}

func (resolver *PostgresExecutionContextResolver) scanExecutionContextRow(
	row pgx.Row,
	result *ExecutionContext,
) error {
	var scopeKind, valueKind, name string
	var keyID *string
	var organizationID, repositoryID, environment string
	var variableValue *string
	var encryptedValue, nonce []byte
	if err := row.Scan(
		&scopeKind, &valueKind, &name, &variableValue, &encryptedValue, &nonce, &keyID,
		&organizationID, &repositoryID, &environment,
	); err != nil {
		return fmt.Errorf("scan Actions execution context: %w", err)
	}
	target, err := executionContextTarget(result, scopeKind, valueKind)
	if err != nil {
		return err
	}
	if valueKind == executionContextValueVariable {
		if variableValue == nil {
			return errors.New("stored Actions variable has no value")
		}
		target[name] = *variableValue
		return nil
	}
	if keyID == nil || *keyID != resolver.keyID {
		storedKeyID := ""
		if keyID != nil {
			storedKeyID = *keyID
		}
		return fmt.Errorf("Actions secret %q uses unavailable encryption key %q", name, storedKeyID)
	}
	plaintext, err := resolver.aead.Open(nil, nonce, encryptedValue, executionContextAAD(
		scopeKind, organizationID, repositoryID, environment, name,
	))
	if err != nil {
		return fmt.Errorf("decrypt Actions secret %q: authentication failed", name)
	}
	target[name] = string(plaintext)
	clear(plaintext)
	return nil
}

func emptyExecutionContext() ExecutionContext {
	return ExecutionContext{
		OrganizationVariables: make(map[string]string),
		RepositoryVariables:   make(map[string]string),
		EnvironmentVariables:  make(map[string]string),
		OrganizationSecrets:   make(map[string]string),
		RepositorySecrets:     make(map[string]string),
		EnvironmentSecrets:    make(map[string]string),
	}
}

func executionContextTarget(
	value *ExecutionContext,
	scopeKind string,
	valueKind string,
) (map[string]string, error) {
	if valueKind != executionContextValueVariable && valueKind != executionContextValueSecret {
		return nil, errors.New("stored Actions execution context kind is invalid")
	}
	switch scopeKind {
	case executionContextScopeOrganization:
		if valueKind == executionContextValueSecret {
			return value.OrganizationSecrets, nil
		}
		return value.OrganizationVariables, nil
	case executionContextScopeRepository:
		if valueKind == executionContextValueSecret {
			return value.RepositorySecrets, nil
		}
		return value.RepositoryVariables, nil
	case executionContextScopeEnvironment:
		if valueKind == executionContextValueSecret {
			return value.EnvironmentSecrets, nil
		}
		return value.EnvironmentVariables, nil
	default:
		return nil, errors.New("stored Actions execution context scope is invalid")
	}
}

func executionContextAAD(
	scopeKind string,
	organizationID string,
	repositoryID string,
	environment string,
	name string,
) []byte {
	return []byte(strings.Join([]string{
		"lorehub-actions-secret-v1", scopeKind, organizationID, repositoryID, environment, name,
	}, "\x00"))
}
