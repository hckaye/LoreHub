package runner

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxActionsJobTokenLifetime = 15 * time.Minute
	maxActionsJobTokenSize     = 16 * 1024
)

var (
	ErrActionsJobTokenInvalid      = errors.New("Actions job token is invalid")
	ErrActionsJobTokenScope        = errors.New("Actions job token scope is not authorized")
	ErrActionsJobTokenUnauthorized = errors.New("Actions job token is not authorized")
)

// JobTokenSigningKeyProvider is implemented by loreauth.RSAKeyProvider.
type JobTokenSigningKeyProvider interface {
	Current() jose.JSONWebKey
	PublicKeys() []jose.JSONWebKey
}

type ActionsJobTokenClaims struct {
	jwt.Claims
	JobID         string `json:"job_id"`
	RunID         string `json:"run_id"`
	Attempt       int    `json:"attempt"`
	RepositoryID  string `json:"repository_id"`
	ActorID       string `json:"actor_id"`
	PrincipalKind string `json:"principal_kind"`
	RESTScope     string `json:"rest_scope"`
	GraphQLScope  string `json:"graphql_scope"`
}

type VerifiedJobToken struct {
	Claims ActionsJobTokenClaims
}

type JobTokenVerifier interface {
	Verify(
		ctx context.Context,
		rawToken string,
		expectedRESTScope string,
		expectedGraphQLScope string,
	) (VerifiedJobToken, error)
}

type jobTokenDatabase interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresJobTokenService struct {
	database jobTokenDatabase
	keys     JobTokenSigningKeyProvider
	issuer   string
	audience string
}

func NewPostgresJobTokenService(
	pool *pgxpool.Pool,
	keys JobTokenSigningKeyProvider,
	issuer string,
	audience string,
) (*PostgresJobTokenService, error) {
	if pool == nil {
		return nil, errors.New("Actions job token PostgreSQL pool is required")
	}
	return newPostgresJobTokenService(pool, keys, issuer, audience)
}

func newPostgresJobTokenService(
	database jobTokenDatabase,
	keys JobTokenSigningKeyProvider,
	issuer string,
	audience string,
) (*PostgresJobTokenService, error) {
	if database == nil {
		return nil, errors.New("Actions job token database is required")
	}
	if keys == nil {
		return nil, errors.New("Actions job token signing keys are required")
	}
	if !validJobTokenValue(issuer, 1024) || !validJobTokenValue(audience, 1024) {
		return nil, errors.New("Actions job token issuer and audience are invalid")
	}
	if strings.Contains(issuer, ",") || strings.Contains(audience, ",") {
		return nil, errors.New("Actions job token issuer and audience must be exact values")
	}
	if err := validateJobTokenSigningKey(keys.Current()); err != nil {
		return nil, err
	}
	if _, err := selectJobTokenVerificationKey(keys.PublicKeys(), keys.Current().KeyID); err != nil {
		return nil, err
	}
	return &PostgresJobTokenService{database: database, keys: keys, issuer: issuer, audience: audience}, nil
}

func (service *PostgresJobTokenService) Issue(
	ctx context.Context,
	request JobTokenRequest,
) (JobToken, error) {
	if err := validateJobTokenRequest(ctx, request); err != nil {
		return JobToken{}, err
	}
	if err := validatePostgresJobTokenRequest(request); err != nil {
		return JobToken{}, err
	}
	claims := ActionsJobTokenClaims{
		JobID:         request.JobID,
		RunID:         request.RunID,
		Attempt:       request.Attempt,
		RepositoryID:  request.RepositoryID,
		ActorID:       request.ActorID,
		PrincipalKind: request.ServicePrincipal.Kind,
		RESTScope:     request.RESTScope,
		GraphQLScope:  request.GraphQLScope,
	}
	if err := service.authorize(ctx, claims, request.ServicePrincipal.Subject); err != nil {
		return JobToken{}, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	expiresAt := request.RequestedExpiry.UTC().Truncate(time.Second)
	if expiresAt.After(now.Add(maxActionsJobTokenLifetime)) {
		expiresAt = now.Add(maxActionsJobTokenLifetime)
	}
	if !expiresAt.After(now) {
		return JobToken{}, errors.New("Actions job token expiry is no longer valid")
	}
	claims.Claims = jwt.Claims{
		Issuer:   service.issuer,
		Subject:  request.ServicePrincipal.Subject,
		Audience: jwt.Audience{service.audience},
		IssuedAt: jwt.NewNumericDate(now),
		Expiry:   jwt.NewNumericDate(expiresAt),
	}
	rawToken, err := service.sign(claims)
	if err != nil {
		return JobToken{}, err
	}
	return JobToken{
		RepositoryID: request.RepositoryID,
		Token:        rawToken,
		Subject:      request.ServicePrincipal.Subject,
		ExpiresAt:    expiresAt,
	}, nil
}

func (service *PostgresJobTokenService) Verify(
	ctx context.Context,
	rawToken string,
	expectedRESTScope string,
	expectedGraphQLScope string,
) (VerifiedJobToken, error) {
	if err := ctx.Err(); err != nil {
		return VerifiedJobToken{}, err
	}
	if !validJobTokenScope(expectedRESTScope) || !validJobTokenScope(expectedGraphQLScope) {
		return VerifiedJobToken{}, ErrActionsJobTokenScope
	}
	if strings.TrimSpace(rawToken) == "" || len(rawToken) > maxActionsJobTokenSize {
		return VerifiedJobToken{}, ErrActionsJobTokenInvalid
	}
	parsed, err := jwt.ParseSigned(rawToken, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil || len(parsed.Headers) != 1 {
		return VerifiedJobToken{}, ErrActionsJobTokenInvalid
	}
	header := parsed.Headers[0]
	if header.Algorithm != string(jose.RS256) || header.KeyID == "" {
		return VerifiedJobToken{}, ErrActionsJobTokenInvalid
	}
	key, err := selectJobTokenVerificationKey(service.keys.PublicKeys(), header.KeyID)
	if err != nil {
		return VerifiedJobToken{}, ErrActionsJobTokenInvalid
	}
	var claims ActionsJobTokenClaims
	if err := parsed.Claims(key.Key, &claims); err != nil {
		return VerifiedJobToken{}, ErrActionsJobTokenInvalid
	}
	if err := service.validateClaims(claims, expectedRESTScope, expectedGraphQLScope); err != nil {
		return VerifiedJobToken{}, err
	}
	if err := service.authorize(ctx, claims, claims.Subject); err != nil {
		return VerifiedJobToken{}, err
	}
	return VerifiedJobToken{Claims: claims}, nil
}

func (service *PostgresJobTokenService) sign(claims ActionsJobTokenClaims) (string, error) {
	current := service.keys.Current()
	if err := validateJobTokenSigningKey(current); err != nil {
		return "", err
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: current.Key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", current.KeyID),
	)
	if err != nil {
		return "", fmt.Errorf("create Actions job token signer: %w", err)
	}
	rawToken, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		return "", fmt.Errorf("sign Actions job token: %w", err)
	}
	return rawToken, nil
}

func (service *PostgresJobTokenService) validateClaims(
	claims ActionsJobTokenClaims,
	expectedRESTScope string,
	expectedGraphQLScope string,
) error {
	now := time.Now().UTC()
	if err := claims.Validate(jwt.Expected{
		Issuer:      service.issuer,
		AnyAudience: jwt.Audience{service.audience},
		Time:        now,
	}); err != nil {
		return ErrActionsJobTokenInvalid
	}
	if claims.Issuer != service.issuer || len(claims.Audience) != 1 ||
		claims.Audience[0] != service.audience || claims.Subject == "" {
		return ErrActionsJobTokenInvalid
	}
	if claims.IssuedAt == nil || claims.Expiry == nil || claims.IssuedAt.Time().After(now) ||
		!claims.Expiry.Time().After(now) || !claims.Expiry.Time().After(claims.IssuedAt.Time()) ||
		claims.Expiry.Time().Sub(claims.IssuedAt.Time()) > maxActionsJobTokenLifetime {
		return ErrActionsJobTokenInvalid
	}
	if claims.RESTScope != expectedRESTScope || claims.GraphQLScope != expectedGraphQLScope {
		return ErrActionsJobTokenScope
	}
	request := JobTokenRequest{
		JobID:            claims.JobID,
		RunID:            claims.RunID,
		Attempt:          claims.Attempt,
		RepositoryID:     claims.RepositoryID,
		ActorID:          claims.ActorID,
		ServicePrincipal: CredentialPrincipal{Kind: claims.PrincipalKind, Subject: claims.Subject},
		RESTScope:        claims.RESTScope,
		GraphQLScope:     claims.GraphQLScope,
		RequestedExpiry:  claims.Expiry.Time(),
	}
	if err := validatePostgresJobTokenRequest(request); err != nil {
		return ErrActionsJobTokenInvalid
	}
	return nil
}

func (service *PostgresJobTokenService) authorize(
	ctx context.Context,
	claims ActionsJobTokenClaims,
	principalSubject string,
) error {
	jobID, runID, repositoryID, actorID, err := parseJobTokenIDs(claims)
	if err != nil {
		return ErrActionsJobTokenUnauthorized
	}
	var allowed bool
	err = service.database.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM ci_jobs job
			JOIN ci_runs run ON run.id = job.run_id
			JOIN repositories repository ON repository.id = run.repository_id
			JOIN organizations organization ON organization.id = repository.organization_id
			JOIN service_principals principal
			  ON (principal.id::text = $6 OR principal.name = $6)
			 AND principal.kind = 'ci_runner' AND principal.active
			JOIN service_principal_repository_grants repository_grant
			  ON repository_grant.principal_id = principal.id
			 AND repository_grant.repository_id = repository.id
			WHERE job.id = $1 AND run.id = $2 AND job.attempt = $3
			  AND repository.id = $4 AND COALESCE(run.actor_id::text, '') = $5
			  AND job.status = 'in_progress' AND run.status = 'in_progress'
			  AND run.cancel_requested = false
			  AND job.lease_owner IS NOT NULL AND btrim(job.lease_owner) <> ''
			  AND job.lease_expires_at > now()
			  AND repository.archived_at IS NULL AND repository.lifecycle_state = 'active'
			  AND organization.active AND repository_grant.active
			  AND 'read'::varchar = ANY(repository_grant.permissions)
		)
	`, jobID, runID, claims.Attempt, repositoryID, actorID, principalSubject).Scan(&allowed)
	if err != nil {
		return fmt.Errorf("check Actions job token authorization: %w", err)
	}
	if !allowed {
		return ErrActionsJobTokenUnauthorized
	}
	return nil
}

func validatePostgresJobTokenRequest(request JobTokenRequest) error {
	if request.Attempt <= 0 || request.ServicePrincipal.Kind != "service" ||
		!validJobTokenValue(request.ServicePrincipal.Subject, 128) {
		return errors.New("Actions job token requires an exact service principal")
	}
	if !validJobTokenScope(request.RESTScope) || !validJobTokenScope(request.GraphQLScope) {
		return errors.New("Actions job token scopes are invalid")
	}
	claims := ActionsJobTokenClaims{
		JobID: request.JobID, RunID: request.RunID, Attempt: request.Attempt,
		RepositoryID: request.RepositoryID, ActorID: request.ActorID,
	}
	_, _, _, _, err := parseJobTokenIDs(claims)
	return err
}

func parseJobTokenIDs(claims ActionsJobTokenClaims) (uuid.UUID, uuid.UUID, uuid.UUID, string, error) {
	jobID, err := uuid.Parse(claims.JobID)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, "", errors.New("Actions job ID is invalid")
	}
	runID, err := uuid.Parse(claims.RunID)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, "", errors.New("Actions run ID is invalid")
	}
	repositoryID, err := uuid.Parse(claims.RepositoryID)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, "", errors.New("Actions repository ID is invalid")
	}
	actorID := ""
	if claims.ActorID != "" {
		parsedActorID, err := uuid.Parse(claims.ActorID)
		if err != nil {
			return uuid.Nil, uuid.Nil, uuid.Nil, "", errors.New("Actions actor ID is invalid")
		}
		actorID = parsedActorID.String()
	}
	return jobID, runID, repositoryID, actorID, nil
}

func validateJobTokenSigningKey(key jose.JSONWebKey) error {
	privateKey, ok := key.Key.(*rsa.PrivateKey)
	if !ok || privateKey.N == nil || privateKey.N.BitLen() < 2048 || privateKey.Validate() != nil ||
		!validJobTokenValue(key.KeyID, 256) || key.Use != "sig" || key.Algorithm != string(jose.RS256) {
		return errors.New("Actions job token signing key must be a named RS256 RSA key")
	}
	return nil
}

func selectJobTokenVerificationKey(keys []jose.JSONWebKey, keyID string) (jose.JSONWebKey, error) {
	var selected jose.JSONWebKey
	matches := 0
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if !validJobTokenValue(key.KeyID, 256) || seen[key.KeyID] {
			return jose.JSONWebKey{}, errors.New("Actions job token verification keys are invalid")
		}
		seen[key.KeyID] = true
		if key.KeyID != keyID {
			continue
		}
		if key.Use != "sig" || key.Algorithm != string(jose.RS256) || !validJobTokenRSAKey(key.Key) {
			return jose.JSONWebKey{}, errors.New("Actions job token verification key is invalid")
		}
		selected = key
		matches++
	}
	if matches != 1 {
		return jose.JSONWebKey{}, errors.New("Actions job token verification key was not found exactly once")
	}
	return selected, nil
}

func validJobTokenRSAKey(key any) bool {
	switch value := key.(type) {
	case *rsa.PublicKey:
		return value.N != nil && value.N.BitLen() >= 2048
	case *rsa.PrivateKey:
		return value.N != nil && value.N.BitLen() >= 2048 && value.Validate() == nil
	default:
		return false
	}
}

func validJobTokenScope(value string) bool {
	return validJobTokenValue(value, 256) && !strings.ContainsFunc(value, unicode.IsSpace)
}

func validJobTokenValue(value string, maxLength int) bool {
	if value == "" || len(value) > maxLength || strings.TrimSpace(value) != value {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsControl)
}
