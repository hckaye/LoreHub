package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lorehub/lorehub/services/api/internal/auth"
)

const (
	LoreServerStatusActive  = "active"
	LoreServerStatusRevoked = "revoked"

	LoreServerSelectionExplicitUnavailable = "explicit_server_unavailable"
	LoreServerSelectionDefaultUnavailable  = "default_server_unavailable"
	LoreServerSelectionEntitlementRequired = "hosted_lore_server_entitlement_required"
	LoreServerSelectionHostedDisabled      = "hosted_lore_server_disabled"
	LoreServerSelectionNoneAvailable       = "no_lore_server_available"

	loreServerRegistrationMaxAge = time.Hour
	loreServerCredentialMaxAge   = 5 * 366 * 24 * time.Hour
	loreServerHealthMaxBytes     = 16 << 10
)

var loreServerKeyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type LoreServer struct {
	ID                   string         `json:"id"`
	InstanceScope        bool           `json:"instanceScope"`
	OrganizationID       *string        `json:"organizationId"`
	UserID               *string        `json:"userId"`
	Name                 string         `json:"name"`
	PublicURL            string         `json:"publicUrl"`
	Status               string         `json:"status"`
	CredentialExpiresAt  *time.Time     `json:"credentialExpiresAt"`
	CredentialLastUsedAt *time.Time     `json:"credentialLastUsedAt"`
	RevokedAt            *time.Time     `json:"revokedAt"`
	LoreBuildVersion     string         `json:"loreBuildVersion"`
	LastSeenAt           *time.Time     `json:"lastSeenAt"`
	HealthMetadata       map[string]any `json:"healthMetadata"`
	CreatedAt            time.Time      `json:"createdAt"`
	UpdatedAt            time.Time      `json:"updatedAt"`
}

type LoreServerRegistrationToken struct {
	ID             string     `json:"id"`
	InstanceScope  bool       `json:"instanceScope"`
	OrganizationID *string    `json:"organizationId"`
	UserID         *string    `json:"userId"`
	ExpiresAt      time.Time  `json:"expiresAt"`
	ConsumedAt     *time.Time `json:"consumedAt"`
	CreatedBy      string     `json:"createdBy"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type CreateLoreServerRegistrationTokenInput struct {
	Digest    []byte
	ExpiresAt time.Time
}

type RegisterLoreServerInput struct {
	Name                string
	PublicURL           string
	CredentialDigest    []byte
	CredentialKeyID     string
	CredentialExpiresAt time.Time
	LoreBuildVersion    string
	HookModuleVersion   string
	HealthMetadata      map[string]any
	AllowPrivateServers bool
}

type LoreServerSelectionError struct {
	Reason string
}

func (err *LoreServerSelectionError) Error() string {
	switch err.Reason {
	case LoreServerSelectionExplicitUnavailable:
		return "the selected Lore server is not active or is not available to this organization"
	case LoreServerSelectionDefaultUnavailable:
		return "the organization's default Lore server is not active"
	case LoreServerSelectionEntitlementRequired:
		return "register a Lore server or obtain the hosted Lore server entitlement"
	case LoreServerSelectionHostedDisabled:
		return "the hosted Lore server is disabled on this instance"
	default:
		return "no active Lore server is available to this organization"
	}
}

func (store *Store) EnsureInstanceLoreServer(ctx context.Context, publicURL string) (LoreServer, error) {
	normalizedURL, err := validateLoreServerURL(publicURL, true)
	if err != nil {
		return LoreServer{}, err
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return LoreServer{}, fmt.Errorf("begin instance Lore server reconciliation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := transaction.Exec(ctx, `LOCK TABLE lore_servers IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return LoreServer{}, fmt.Errorf("lock instance Lore server reconciliation: %w", err)
	}

	server, err := scanLoreServer(transaction.QueryRow(ctx, loreServerSelect+`
		WHERE server.instance_scope
		FOR UPDATE
	`))
	if errors.Is(err, pgx.ErrNoRows) {
		serverID := uuid.NewString()
		server, err = scanLoreServer(transaction.QueryRow(ctx, loreServerSelectFromInsert+`
			INSERT INTO lore_servers (
				id, instance_scope, name, public_url, status
			) VALUES ($1, true, 'LoreHub managed Lore Server', $2, 'active')
			RETURNING id, instance_scope, organization_id, user_id, name, public_url, status,
			          credential_expires_at, credential_last_used_at, revoked_at, lore_build_version,
			          last_seen_at, health_metadata, created_at, updated_at
		) SELECT * FROM inserted
		`, serverID, normalizedURL))
	} else if err == nil {
		server, err = scanLoreServer(transaction.QueryRow(ctx, loreServerSelectFromInsert+`
			UPDATE lore_servers
			SET name = 'LoreHub managed Lore Server', public_url = $2, status = 'active',
			    revoked_at = NULL, updated_at = now()
			WHERE id = $1
			RETURNING id, instance_scope, organization_id, user_id, name, public_url, status,
			          credential_expires_at, credential_last_used_at, revoked_at, lore_build_version,
			          last_seen_at, health_metadata, created_at, updated_at
		) SELECT * FROM inserted
		`, server.ID, normalizedURL))
	}
	if err != nil {
		return LoreServer{}, fmt.Errorf("ensure instance Lore server: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE repositories SET lore_server_id = $1 WHERE lore_server_id IS NULL
	`, server.ID); err != nil {
		return LoreServer{}, fmt.Errorf("backfill repository Lore server assignments: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return LoreServer{}, fmt.Errorf("commit instance Lore server reconciliation: %w", err)
	}
	return server, nil
}

func (store *Store) CreateLoreServerRegistrationToken(
	ctx context.Context,
	actor User,
	organizationSlug string,
	input CreateLoreServerRegistrationTokenInput,
) (LoreServerRegistrationToken, error) {
	if len(input.Digest) != 32 || input.ExpiresAt.IsZero() {
		return LoreServerRegistrationToken{}, ErrInvalidInput
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return LoreServerRegistrationToken{}, fmt.Errorf("begin Lore server registration token creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	organizationID, err := requireLoreServerOrganizationRole(ctx, transaction, actor.ID, organizationSlug, true)
	if err != nil {
		return LoreServerRegistrationToken{}, err
	}
	var now time.Time
	if err := transaction.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
		return LoreServerRegistrationToken{}, fmt.Errorf("read registration token creation time: %w", err)
	}
	if !input.ExpiresAt.After(now.Add(time.Minute)) ||
		input.ExpiresAt.After(now.Add(loreServerRegistrationMaxAge)) {
		return LoreServerRegistrationToken{}, ErrInvalidInput
	}
	token := LoreServerRegistrationToken{
		ID: uuid.NewString(), OrganizationID: &organizationID, ExpiresAt: input.ExpiresAt.UTC(),
		CreatedBy: actor.ID, CreatedAt: now.UTC(),
	}
	if _, err := transaction.Exec(ctx, `
		INSERT INTO lore_server_registration_tokens (
			id, organization_id, token_digest, expires_at, created_by, created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, token.ID, organizationID, input.Digest, token.ExpiresAt, token.CreatedBy, token.CreatedAt); err != nil {
		return LoreServerRegistrationToken{}, fmt.Errorf("create Lore server registration token: %w", err)
	}
	if err := insertAudit(ctx, transaction, actor.ID, organizationID, "",
		"lore_server.registration_token.create", "lore_server_registration_token", token.ID); err != nil {
		return LoreServerRegistrationToken{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return LoreServerRegistrationToken{}, fmt.Errorf("commit Lore server registration token creation: %w", err)
	}
	return token, nil
}

func (store *Store) ConsumeLoreServerRegistrationToken(
	ctx context.Context,
	digest []byte,
	consumedAt time.Time,
) (LoreServerRegistrationToken, error) {
	if len(digest) != 32 || consumedAt.IsZero() {
		return LoreServerRegistrationToken{}, auth.ErrInvalidLoreServerRegistrationToken
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return LoreServerRegistrationToken{}, fmt.Errorf("begin Lore server registration token consumption: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	token, err := consumeRegistrationToken(ctx, transaction, digest, consumedAt)
	if err != nil {
		return LoreServerRegistrationToken{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return LoreServerRegistrationToken{}, fmt.Errorf("commit Lore server registration token consumption: %w", err)
	}
	return token, nil
}

func (store *Store) RegisterServer(
	ctx context.Context,
	registrationDigest []byte,
	input RegisterLoreServerInput,
) (LoreServer, error) {
	normalizedURL, health, err := validateRegisterLoreServerInput(input)
	if err != nil {
		return LoreServer{}, err
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return LoreServer{}, fmt.Errorf("begin Lore server registration: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	now := time.Now().UTC()
	token, err := consumeRegistrationToken(ctx, transaction, registrationDigest, now)
	if err != nil {
		return LoreServer{}, err
	}
	if token.OrganizationID == nil || token.InstanceScope || token.UserID != nil {
		return LoreServer{}, auth.ErrInvalidLoreServerRegistrationToken
	}
	var activeOrganization bool
	if err := transaction.QueryRow(ctx, `
		SELECT active FROM organizations WHERE id = $1 FOR KEY SHARE
	`, *token.OrganizationID).Scan(&activeOrganization); err != nil || !activeOrganization {
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return LoreServer{}, fmt.Errorf("validate Lore server registration organization: %w", err)
		}
		return LoreServer{}, auth.ErrInvalidLoreServerRegistrationToken
	}
	serverID := uuid.NewString()
	server, err := scanLoreServer(transaction.QueryRow(ctx, loreServerSelectFromInsert+`
		INSERT INTO lore_servers (
			id, organization_id, name, public_url, status, credential_digest,
			credential_key_id, credential_expires_at, lore_build_version, health_metadata
		) VALUES ($1, $2, $3, $4, 'active', $5, $6, $7, $8, $9)
		RETURNING id, instance_scope, organization_id, user_id, name, public_url, status,
		          credential_expires_at, credential_last_used_at, revoked_at, lore_build_version,
		          last_seen_at, health_metadata, created_at, updated_at
	) SELECT * FROM inserted
	`, serverID, *token.OrganizationID, strings.TrimSpace(input.Name), normalizedURL,
		input.CredentialDigest, input.CredentialKeyID, input.CredentialExpiresAt.UTC(),
		strings.TrimSpace(input.LoreBuildVersion), health))
	if err != nil {
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) && databaseError.Code == "23505" {
			return LoreServer{}, ErrConflict
		}
		return LoreServer{}, fmt.Errorf("register Lore server: %w", err)
	}
	if err := insertAudit(ctx, transaction, token.CreatedBy, *token.OrganizationID, "",
		"lore_server.register", "lore_server", server.ID); err != nil {
		return LoreServer{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return LoreServer{}, fmt.Errorf("commit Lore server registration: %w", err)
	}
	return server, nil
}

func (store *Store) ListServers(
	ctx context.Context,
	actor User,
	organizationSlug string,
) ([]LoreServer, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin Lore server list: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	organizationID, err := requireLoreServerOrganizationRole(ctx, transaction, actor.ID, organizationSlug, false)
	if err != nil {
		return nil, err
	}
	entitled, err := hasOrganizationEntitlement(
		ctx, transaction, organizationID, EntitlementHostedLoreServer,
		store.hostedLoreServerDefaultEnabled,
	)
	if err != nil {
		return nil, err
	}
	rows, err := transaction.Query(ctx, loreServerSelect+`
		WHERE server.organization_id = $1 OR (server.instance_scope AND $2)
		ORDER BY server.instance_scope, server.created_at DESC, server.id
	`, organizationID, entitled)
	if err != nil {
		return nil, fmt.Errorf("list Lore servers: %w", err)
	}
	defer rows.Close()
	servers := make([]LoreServer, 0)
	for rows.Next() {
		server, err := scanLoreServer(rows)
		if err != nil {
			return nil, fmt.Errorf("scan Lore server: %w", err)
		}
		servers = append(servers, server)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Lore servers: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit Lore server list: %w", err)
	}
	return servers, nil
}

func (store *Store) RevokeServer(
	ctx context.Context,
	actor User,
	organizationSlug string,
	serverID string,
) error {
	if _, err := uuid.Parse(serverID); err != nil {
		return ErrNotFound
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin Lore server revocation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	organizationID, err := requireLoreServerOrganizationRole(ctx, transaction, actor.ID, organizationSlug, true)
	if err != nil {
		return err
	}
	command, err := transaction.Exec(ctx, `
		UPDATE lore_servers
		SET status = 'revoked', revoked_at = now(), updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND status = 'active' AND revoked_at IS NULL
	`, serverID, organizationID)
	if err != nil {
		return fmt.Errorf("revoke Lore server: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrNotFound
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE organizations SET default_lore_server_id = NULL, updated_at = now()
		WHERE id = $1 AND default_lore_server_id = $2
	`, organizationID, serverID); err != nil {
		return fmt.Errorf("clear revoked default Lore server: %w", err)
	}
	if err := insertAudit(ctx, transaction, actor.ID, organizationID, "",
		"lore_server.revoke", "lore_server", serverID); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Lore server revocation: %w", err)
	}
	return nil
}

func (store *Store) TouchServerSeen(ctx context.Context, serverID string, seenAt time.Time) error {
	if _, err := uuid.Parse(serverID); err != nil || seenAt.IsZero() {
		return auth.ErrInvalidLoreServerCredential
	}
	command, err := store.pool.Exec(ctx, `
		UPDATE lore_servers
		SET last_seen_at = $2, updated_at = $2
		WHERE id = $1 AND status = 'active' AND revoked_at IS NULL
	`, serverID, seenAt.UTC())
	if err != nil {
		return fmt.Errorf("touch Lore server heartbeat: %w", err)
	}
	if command.RowsAffected() != 1 {
		return auth.ErrInvalidLoreServerCredential
	}
	return nil
}

func (store *Store) UpdateServerHealth(
	ctx context.Context,
	serverID string,
	seenAt time.Time,
	loreBuildVersion string,
	hookModuleVersion string,
	healthMetadata map[string]any,
) error {
	if _, err := uuid.Parse(serverID); err != nil || seenAt.IsZero() ||
		!supportedLoreBuildVersion(loreBuildVersion) || !supportedHookModuleVersion(hookModuleVersion) {
		return ErrInvalidInput
	}
	health, err := encodedHealthMetadata(healthMetadata, hookModuleVersion)
	if err != nil {
		return err
	}
	command, err := store.pool.Exec(ctx, `
		UPDATE lore_servers
		SET last_seen_at = $2, lore_build_version = $3, health_metadata = $4, updated_at = $2
		WHERE id = $1 AND status = 'active' AND revoked_at IS NULL
	`, serverID, seenAt.UTC(), strings.TrimSpace(loreBuildVersion), health)
	if err != nil {
		return fmt.Errorf("update Lore server health: %w", err)
	}
	if command.RowsAffected() != 1 {
		return auth.ErrInvalidLoreServerCredential
	}
	return nil
}

func (store *Store) AuthenticateServer(
	ctx context.Context,
	digest []byte,
	keyID string,
	usedAt time.Time,
) (LoreServer, error) {
	if len(digest) != 32 || !loreServerKeyIDPattern.MatchString(keyID) || usedAt.IsZero() {
		return LoreServer{}, auth.ErrInvalidLoreServerCredential
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return LoreServer{}, fmt.Errorf("begin Lore server authentication: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	server, err := scanLoreServer(transaction.QueryRow(ctx, loreServerSelect+`
		WHERE server.credential_digest = $1 AND server.credential_key_id = $2
		  AND server.status = 'active' AND server.revoked_at IS NULL
		  AND server.credential_expires_at > $3
		FOR UPDATE
	`, digest, keyID, usedAt.UTC()))
	if errors.Is(err, pgx.ErrNoRows) {
		return LoreServer{}, auth.ErrInvalidLoreServerCredential
	}
	if err != nil {
		return LoreServer{}, fmt.Errorf("authenticate Lore server: %w", err)
	}
	if server.CredentialLastUsedAt == nil || server.CredentialLastUsedAt.Before(usedAt.Add(-5*time.Minute)) {
		if _, err := transaction.Exec(ctx, `
			UPDATE lore_servers SET credential_last_used_at = $2 WHERE id = $1
		`, server.ID, usedAt.UTC()); err != nil {
			return LoreServer{}, fmt.Errorf("record Lore server credential use: %w", err)
		}
		lastUsed := usedAt.UTC()
		server.CredentialLastUsedAt = &lastUsed
	}
	if err := transaction.Commit(ctx); err != nil {
		return LoreServer{}, fmt.Errorf("commit Lore server authentication: %w", err)
	}
	return server, nil
}

func (store *Store) GetOrganizationDefaultServer(
	ctx context.Context,
	actor User,
	organizationSlug string,
) (*LoreServer, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin default Lore server read: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	organizationID, err := requireLoreServerOrganizationRole(ctx, transaction, actor.ID, organizationSlug, false)
	if err != nil {
		return nil, err
	}
	var serverID *string
	if err := transaction.QueryRow(ctx, `
		SELECT default_lore_server_id FROM organizations WHERE id = $1
	`, organizationID).Scan(&serverID); err != nil {
		return nil, fmt.Errorf("read default Lore server setting: %w", err)
	}
	if serverID == nil {
		if err := transaction.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit default Lore server read: %w", err)
		}
		return nil, nil
	}
	server, err := resolveServerForNewRepository(
		ctx, transaction, organizationID, *serverID, store.hostedLoreServerDefaultEnabled,
	)
	if err != nil {
		return nil, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit default Lore server read: %w", err)
	}
	return &server, nil
}

func (store *Store) SetOrganizationDefaultServer(
	ctx context.Context,
	actor User,
	organizationSlug string,
	serverID string,
) (*LoreServer, error) {
	serverID = strings.TrimSpace(serverID)
	if serverID != "" {
		if _, err := uuid.Parse(serverID); err != nil {
			return nil, ErrInvalidInput
		}
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, fmt.Errorf("begin default Lore server update: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	organizationID, err := requireLoreServerOrganizationRole(ctx, transaction, actor.ID, organizationSlug, true)
	if err != nil {
		return nil, err
	}
	var server *LoreServer
	if serverID != "" {
		selected, resolveErr := resolveServerForNewRepository(
			ctx, transaction, organizationID, serverID, store.hostedLoreServerDefaultEnabled,
		)
		if resolveErr != nil {
			return nil, resolveErr
		}
		server = &selected
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE organizations
		SET default_lore_server_id = NULLIF($2, '')::uuid, updated_at = now()
		WHERE id = $1
	`, organizationID, serverID); err != nil {
		return nil, fmt.Errorf("set default Lore server: %w", err)
	}
	action := "lore_server.default.clear"
	targetType := "organization"
	targetID := organizationID
	if server != nil {
		action = "lore_server.default.set"
		targetType = "lore_server"
		targetID = server.ID
	}
	if err := insertAudit(ctx, transaction, actor.ID, organizationID, "",
		action, targetType, targetID); err != nil {
		return nil, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit default Lore server update: %w", err)
	}
	return server, nil
}

func (store *Store) ResolveServerForNewRepository(
	ctx context.Context,
	organizationID string,
	explicitServerID string,
) (LoreServer, error) {
	if _, err := uuid.Parse(organizationID); err != nil {
		return LoreServer{}, ErrInvalidInput
	}
	return resolveServerForNewRepository(
		ctx, store.pool, organizationID, strings.TrimSpace(explicitServerID),
		store.hostedLoreServerDefaultEnabled,
	)
}

func (store *Store) ValidateRepositoryImportServer(
	ctx context.Context,
	actor User,
	organizationSlug string,
	serverID string,
	repositoryURL string,
) error {
	if _, err := uuid.Parse(serverID); err != nil {
		return ErrInvalidInput
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return fmt.Errorf("begin repository import Lore server validation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	organizationID, err := requireLoreServerOrganizationRole(ctx, transaction, actor.ID, organizationSlug, false)
	if err != nil {
		return err
	}
	server, err := resolveServerForNewRepository(
		ctx, transaction, organizationID, serverID, store.hostedLoreServerDefaultEnabled,
	)
	if err != nil {
		return err
	}
	if !loreServerAuthoritiesMatch(server.PublicURL, repositoryURL) {
		return ErrInvalidInput
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit repository import Lore server validation: %w", err)
	}
	return nil
}

func resolveServerForNewRepository(
	ctx context.Context,
	query loreServerQuery,
	organizationID string,
	explicitServerID string,
	hostedLoreServerDefaultEnabled bool,
) (LoreServer, error) {
	if explicitServerID != "" {
		if _, err := uuid.Parse(explicitServerID); err != nil {
			return LoreServer{}, &LoreServerSelectionError{Reason: LoreServerSelectionExplicitUnavailable}
		}
		server, err := activeVisibleLoreServer(
			ctx, query, organizationID, explicitServerID, hostedLoreServerDefaultEnabled,
		)
		if err == nil {
			return server, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return LoreServer{}, err
		}
		return LoreServer{}, &LoreServerSelectionError{Reason: LoreServerSelectionExplicitUnavailable}
	}

	var defaultServerID *string
	if err := query.QueryRow(ctx, `
		SELECT default_lore_server_id FROM organizations WHERE id = $1
	`, organizationID).Scan(&defaultServerID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LoreServer{}, ErrNotFound
		}
		return LoreServer{}, fmt.Errorf("read organization Lore server default: %w", err)
	}
	if defaultServerID != nil {
		server, err := activeVisibleLoreServer(
			ctx, query, organizationID, *defaultServerID, hostedLoreServerDefaultEnabled,
		)
		if err == nil {
			return server, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return LoreServer{}, err
		}
	}

	hostedEnabled, err := hostedLoreServerEnabledForQuery(
		ctx, query, hostedLoreServerDefaultEnabled,
	)
	if err != nil {
		return LoreServer{}, err
	}
	if !hostedEnabled {
		return LoreServer{}, &LoreServerSelectionError{Reason: LoreServerSelectionHostedDisabled}
	}
	entitled, err := hasOrganizationEntitlement(
		ctx, query, organizationID, EntitlementHostedLoreServer, hostedLoreServerDefaultEnabled,
	)
	if err != nil {
		return LoreServer{}, err
	}
	if !entitled {
		reason := LoreServerSelectionEntitlementRequired
		if defaultServerID != nil {
			reason = LoreServerSelectionDefaultUnavailable
		}
		return LoreServer{}, &LoreServerSelectionError{Reason: reason}
	}
	server, err := scanLoreServer(query.QueryRow(ctx, loreServerSelect+`
		WHERE server.instance_scope AND server.status = 'active' AND server.revoked_at IS NULL
	`))
	if errors.Is(err, pgx.ErrNoRows) {
		return LoreServer{}, &LoreServerSelectionError{Reason: LoreServerSelectionNoneAvailable}
	}
	if err != nil {
		return LoreServer{}, fmt.Errorf("resolve instance Lore server: %w", err)
	}
	return server, nil
}

func activeVisibleLoreServer(
	ctx context.Context,
	query loreServerQuery,
	organizationID string,
	serverID string,
	hostedLoreServerDefaultEnabled bool,
) (LoreServer, error) {
	server, err := scanLoreServer(query.QueryRow(ctx, loreServerSelect+`
		WHERE server.id = $1 AND server.status = 'active' AND server.revoked_at IS NULL
		  AND (server.organization_id = $2 OR server.instance_scope)
		  AND (server.instance_scope OR server.credential_expires_at > now())
	`, serverID, organizationID))
	if err != nil {
		return LoreServer{}, err
	}
	if server.InstanceScope {
		hostedEnabled, err := hostedLoreServerEnabledForQuery(
			ctx, query, hostedLoreServerDefaultEnabled,
		)
		if err != nil {
			return LoreServer{}, err
		}
		if !hostedEnabled {
			return LoreServer{}, &LoreServerSelectionError{Reason: LoreServerSelectionHostedDisabled}
		}
		entitled, err := hasOrganizationEntitlement(
			ctx, query, organizationID, EntitlementHostedLoreServer, hostedLoreServerDefaultEnabled,
		)
		if err != nil {
			return LoreServer{}, err
		}
		if !entitled {
			return LoreServer{}, pgx.ErrNoRows
		}
	}
	return server, nil
}

func consumeRegistrationToken(
	ctx context.Context,
	transaction pgx.Tx,
	digest []byte,
	consumedAt time.Time,
) (LoreServerRegistrationToken, error) {
	if len(digest) != 32 || consumedAt.IsZero() {
		return LoreServerRegistrationToken{}, auth.ErrInvalidLoreServerRegistrationToken
	}
	var token LoreServerRegistrationToken
	err := transaction.QueryRow(ctx, `
		UPDATE lore_server_registration_tokens
		SET consumed_at = $2
		WHERE token_digest = $1 AND consumed_at IS NULL AND expires_at > $2
		RETURNING id, instance_scope, organization_id, user_id, expires_at, consumed_at, created_by, created_at
	`, digest, consumedAt.UTC()).Scan(
		&token.ID, &token.InstanceScope, &token.OrganizationID, &token.UserID, &token.ExpiresAt,
		&token.ConsumedAt, &token.CreatedBy, &token.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return LoreServerRegistrationToken{}, auth.ErrInvalidLoreServerRegistrationToken
	}
	if err != nil {
		return LoreServerRegistrationToken{}, fmt.Errorf("consume Lore server registration token: %w", err)
	}
	return token, nil
}

func requireLoreServerOrganizationRole(
	ctx context.Context,
	transaction pgx.Tx,
	actorID string,
	organizationSlug string,
	ownerOnly bool,
) (string, error) {
	if _, err := uuid.Parse(actorID); err != nil {
		return "", ErrForbidden
	}
	var organizationID, role string
	err := transaction.QueryRow(ctx, `
		SELECT organization.id, membership.role
		FROM organizations organization
		JOIN organization_memberships membership
		  ON membership.organization_id = organization.id AND membership.user_id = $2 AND membership.active
		JOIN users actor ON actor.id = membership.user_id AND actor.status = 'active'
		WHERE organization.slug = $1 AND organization.active
		FOR UPDATE OF organization
	`, organizationSlug, actorID).Scan(&organizationID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrForbidden
	}
	if err != nil {
		return "", fmt.Errorf("authorize Lore server organization setting: %w", err)
	}
	if role != "owner" && (ownerOnly || role != "maintainer") {
		return "", ErrForbidden
	}
	return organizationID, nil
}

func hasOrganizationEntitlement(
	ctx context.Context,
	query loreServerQuery,
	organizationID string,
	feature string,
	hostedLoreServerDefaultEnabled bool,
) (bool, error) {
	if feature == EntitlementHostedLoreServer {
		hostedEnabled, err := hostedLoreServerEnabledForQuery(
			ctx, query, hostedLoreServerDefaultEnabled,
		)
		if err != nil {
			return false, err
		}
		if !hostedEnabled {
			return false, nil
		}
	}
	var entitled bool
	err := query.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM entitlements
			WHERE organization_id = $1 AND user_id IS NULL AND feature = $2 AND revoked_at IS NULL
		)
	`, organizationID, feature).Scan(&entitled)
	if err != nil {
		return false, fmt.Errorf("check organization entitlement: %w", err)
	}
	return entitled, nil
}

func hostedLoreServerEnabledForQuery(
	ctx context.Context,
	query loreServerQuery,
	defaultEnabled bool,
) (bool, error) {
	override, err := readHostedLoreServerOverride(ctx, query)
	if err != nil {
		return false, err
	}
	if override == nil {
		return defaultEnabled, nil
	}
	return *override, nil
}

func validateRegisterLoreServerInput(input RegisterLoreServerInput) (string, []byte, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > 160 || containsControl(name) ||
		len(input.CredentialDigest) != 32 || !loreServerKeyIDPattern.MatchString(input.CredentialKeyID) ||
		input.CredentialExpiresAt.IsZero() || !input.CredentialExpiresAt.After(time.Now().UTC()) ||
		input.CredentialExpiresAt.After(time.Now().UTC().Add(loreServerCredentialMaxAge)) ||
		!supportedLoreBuildVersion(input.LoreBuildVersion) ||
		!supportedHookModuleVersion(input.HookModuleVersion) {
		return "", nil, ErrInvalidInput
	}
	normalizedURL, err := validateLoreServerURL(input.PublicURL, input.AllowPrivateServers)
	if err != nil {
		return "", nil, ErrInvalidInput
	}
	health, err := encodedHealthMetadata(input.HealthMetadata, input.HookModuleVersion)
	if err != nil {
		return "", nil, err
	}
	return normalizedURL, health, nil
}

func validateLoreServerURL(value string, allowPrivate bool) (string, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\t\r\n \\") {
		return "", errors.New("Lore server URL is invalid")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "lores" || parsed.Host == "" || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.Opaque != "" || parsed.Path != "" || parsed.RawPath != "" ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" ||
		strings.ContainsAny(parsed.Host, "\x00\t\r\n /\\") {
		return "", errors.New("Lore server URL must be a fixed lores:// endpoint")
	}
	if port := parsed.Port(); port != "" {
		portNumber, conversionErr := strconv.Atoi(port)
		if conversionErr != nil || portNumber < 1 || portNumber > 65535 {
			return "", errors.New("Lore server URL port is invalid")
		}
	} else if strings.HasSuffix(parsed.Host, ":") {
		return "", errors.New("Lore server URL port is invalid")
	}
	hostname := parsed.Hostname()
	if strings.Contains(hostname, "%") {
		return "", errors.New("Lore server URL address zones are invalid")
	}
	address, addressErr := netip.ParseAddr(hostname)
	if addressErr == nil && !allowPrivate && restrictedLoreServerIP(address) {
		return "", errors.New("Lore server URL must not use a private or reserved IP address")
	}
	host := strings.ToLower(parsed.Host)
	return (&url.URL{Scheme: "lores", Host: host}).String(), nil
}

func loreServerAuthoritiesMatch(serverURL string, repositoryURL string) bool {
	server, err := url.Parse(serverURL)
	if err != nil || server.Scheme != "lores" || server.Host == "" || server.Path != "" {
		return false
	}
	repository, err := url.Parse(repositoryURL)
	if err != nil || repository.Scheme != "lores" || repository.Host == "" || repository.Path == "" {
		return false
	}
	return strings.EqualFold(server.Host, repository.Host)
}

func restrictedLoreServerIP(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsUnspecified() ||
		address.IsMulticast() {
		return true
	}
	for _, prefix := range restrictedLoreServerPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

var restrictedLoreServerPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func supportedLoreBuildVersion(value string) bool {
	major, minor, patch, ok := semanticVersion(value)
	return ok && major == 0 && minor == 8 && patch >= 6
}

func supportedHookModuleVersion(value string) bool {
	major, _, _, ok := semanticVersion(value)
	return ok && major == 1
}

func semanticVersion(value string) (int, int, int, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "v"))
	core, _, _ := strings.Cut(value, "+")
	core, _, _ = strings.Cut(core, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	parsed := [3]int{}
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return 0, 0, 0, false
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return 0, 0, 0, false
		}
		parsed[index] = value
	}
	return parsed[0], parsed[1], parsed[2], true
}

func encodedHealthMetadata(metadata map[string]any, hookModuleVersion string) ([]byte, error) {
	copy := make(map[string]any, len(metadata)+1)
	for key, value := range metadata {
		copy[key] = value
	}
	copy["hookModuleVersion"] = strings.TrimSpace(hookModuleVersion)
	encoded, err := json.Marshal(copy)
	if err != nil || len(encoded) > loreServerHealthMaxBytes {
		return nil, ErrInvalidInput
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil || object == nil {
		return nil, ErrInvalidInput
	}
	return encoded, nil
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

type loreServerQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

const loreServerSelect = `
	SELECT server.id, server.instance_scope, server.organization_id, server.user_id,
	       server.name, server.public_url, server.status, server.credential_expires_at,
	       server.credential_last_used_at, server.revoked_at, server.lore_build_version,
	       server.last_seen_at, server.health_metadata, server.created_at, server.updated_at
	FROM lore_servers server
`

const loreServerSelectFromInsert = `
	WITH inserted AS (
`

func scanLoreServer(row rowScanner) (LoreServer, error) {
	var server LoreServer
	var health []byte
	err := row.Scan(
		&server.ID, &server.InstanceScope, &server.OrganizationID, &server.UserID, &server.Name,
		&server.PublicURL, &server.Status, &server.CredentialExpiresAt, &server.CredentialLastUsedAt,
		&server.RevokedAt, &server.LoreBuildVersion, &server.LastSeenAt, &health,
		&server.CreatedAt, &server.UpdatedAt,
	)
	if err == nil {
		err = json.Unmarshal(health, &server.HealthMetadata)
	}
	return server, err
}
