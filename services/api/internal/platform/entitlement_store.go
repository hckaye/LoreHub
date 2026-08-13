package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	EntitlementHostedLoreServer = "hosted_lore_server"
	EntitlementHostedRunners    = "hosted_runners"
)

type EntitlementSubject struct {
	OrganizationID string
	UserID         string
}

type Entitlement struct {
	OrganizationID *string    `json:"organizationId"`
	UserID         *string    `json:"userId"`
	Feature        string     `json:"feature"`
	GrantedBy      *string    `json:"grantedBy"`
	GrantSource    string     `json:"grantSource"`
	CreatedAt      time.Time  `json:"createdAt"`
	RevokedAt      *time.Time `json:"revokedAt"`
}

func (store *Store) Grant(
	ctx context.Context,
	actor User,
	subject EntitlementSubject,
	feature string,
) (Entitlement, error) {
	normalizedSubject, ok := normalizeEntitlementSubject(subject)
	if !ok || !validEntitlementFeature(feature) {
		return Entitlement{}, ErrInvalidInput
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Entitlement{}, fmt.Errorf("begin entitlement grant: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if err := requireActiveEntitlementActor(ctx, transaction, actor.ID); err != nil {
		return Entitlement{}, err
	}
	if err := lockEntitlementSubject(ctx, transaction, normalizedSubject); err != nil {
		return Entitlement{}, err
	}

	var entitlement Entitlement
	err = transaction.QueryRow(ctx, `
		INSERT INTO entitlements (
			organization_id, user_id, feature, granted_by, grant_source
		) VALUES (NULLIF($1, '')::uuid, NULLIF($2, '')::uuid, $3, $4, 'admin')
		RETURNING organization_id, user_id, feature, granted_by, grant_source, created_at, revoked_at
	`, normalizedSubject.OrganizationID, normalizedSubject.UserID, feature, actor.ID).Scan(
		&entitlement.OrganizationID,
		&entitlement.UserID,
		&entitlement.Feature,
		&entitlement.GrantedBy,
		&entitlement.GrantSource,
		&entitlement.CreatedAt,
		&entitlement.RevokedAt,
	)
	if err != nil {
		return Entitlement{}, translateEntitlementGrantError(err)
	}
	if err := auditEntitlementChange(
		ctx, transaction, actor.ID, normalizedSubject, "entitlement.grant", feature,
	); err != nil {
		return Entitlement{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return Entitlement{}, fmt.Errorf("commit entitlement grant: %w", err)
	}
	return entitlement, nil
}

func (store *Store) Revoke(
	ctx context.Context,
	actor User,
	subject EntitlementSubject,
	feature string,
) error {
	normalizedSubject, ok := normalizeEntitlementSubject(subject)
	if !ok || !validEntitlementFeature(feature) {
		return ErrInvalidInput
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin entitlement revocation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if err := requireActiveEntitlementActor(ctx, transaction, actor.ID); err != nil {
		return err
	}
	command, err := transaction.Exec(ctx, `
		UPDATE entitlements
		SET revoked_at = now()
		WHERE organization_id IS NOT DISTINCT FROM NULLIF($1, '')::uuid
		  AND user_id IS NOT DISTINCT FROM NULLIF($2, '')::uuid
		  AND feature = $3
		  AND revoked_at IS NULL
	`, normalizedSubject.OrganizationID, normalizedSubject.UserID, feature)
	if err != nil {
		return fmt.Errorf("revoke entitlement: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrNotFound
	}
	if err := auditEntitlementChange(
		ctx, transaction, actor.ID, normalizedSubject, "entitlement.revoke", feature,
	); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit entitlement revocation: %w", err)
	}
	return nil
}

func (store *Store) HasEntitlement(
	ctx context.Context,
	subject EntitlementSubject,
	feature string,
) (bool, error) {
	normalizedSubject, ok := normalizeEntitlementSubject(subject)
	if !ok || !validEntitlementFeature(feature) {
		return false, ErrInvalidInput
	}
	var entitled bool
	err := store.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM entitlements
			WHERE organization_id IS NOT DISTINCT FROM NULLIF($1, '')::uuid
			  AND user_id IS NOT DISTINCT FROM NULLIF($2, '')::uuid
			  AND feature = $3
			  AND revoked_at IS NULL
		)
	`, normalizedSubject.OrganizationID, normalizedSubject.UserID, feature).Scan(&entitled)
	if err != nil {
		return false, fmt.Errorf("check entitlement: %w", err)
	}
	return entitled, nil
}

func (store *Store) List(ctx context.Context) ([]Entitlement, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT organization_id, user_id, feature, granted_by, grant_source, created_at, revoked_at
		FROM entitlements
		ORDER BY created_at DESC, COALESCE(organization_id::text, user_id::text), feature
	`)
	if err != nil {
		return nil, fmt.Errorf("list entitlements: %w", err)
	}
	defer rows.Close()

	entitlements := make([]Entitlement, 0)
	for rows.Next() {
		var entitlement Entitlement
		if err := rows.Scan(
			&entitlement.OrganizationID,
			&entitlement.UserID,
			&entitlement.Feature,
			&entitlement.GrantedBy,
			&entitlement.GrantSource,
			&entitlement.CreatedAt,
			&entitlement.RevokedAt,
		); err != nil {
			return nil, fmt.Errorf("scan entitlement: %w", err)
		}
		entitlements = append(entitlements, entitlement)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entitlements: %w", err)
	}
	return entitlements, nil
}

func normalizeEntitlementSubject(subject EntitlementSubject) (EntitlementSubject, bool) {
	organizationID := strings.TrimSpace(subject.OrganizationID)
	userID := strings.TrimSpace(subject.UserID)
	if (organizationID == "") == (userID == "") {
		return EntitlementSubject{}, false
	}
	if organizationID != "" {
		parsed, err := uuid.Parse(organizationID)
		if err != nil {
			return EntitlementSubject{}, false
		}
		organizationID = parsed.String()
	} else {
		parsed, err := uuid.Parse(userID)
		if err != nil {
			return EntitlementSubject{}, false
		}
		userID = parsed.String()
	}
	return EntitlementSubject{OrganizationID: organizationID, UserID: userID}, true
}

func validEntitlementFeature(feature string) bool {
	return feature == EntitlementHostedLoreServer || feature == EntitlementHostedRunners
}

func requireActiveEntitlementActor(ctx context.Context, transaction pgx.Tx, actorID string) error {
	parsedActorID, err := uuid.Parse(strings.TrimSpace(actorID))
	if err != nil {
		return ErrForbidden
	}
	var found string
	err = transaction.QueryRow(ctx, `
		SELECT id FROM users WHERE id = $1 AND status = 'active' FOR KEY SHARE
	`, parsedActorID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("find active entitlement actor: %w", err)
	}
	return nil
}

func lockEntitlementSubject(ctx context.Context, transaction pgx.Tx, subject EntitlementSubject) error {
	var found string
	var err error
	if subject.OrganizationID != "" {
		err = transaction.QueryRow(ctx, `
			SELECT id FROM organizations WHERE id = $1 FOR KEY SHARE
		`, subject.OrganizationID).Scan(&found)
	} else {
		err = transaction.QueryRow(ctx, `
			SELECT id FROM users WHERE id = $1 FOR KEY SHARE
		`, subject.UserID).Scan(&found)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find entitlement subject: %w", err)
	}
	return nil
}

func translateEntitlementGrantError(err error) error {
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		switch databaseError.Code {
		case "23505":
			return fmt.Errorf("grant entitlement: %w", ErrConflict)
		case "23503":
			return fmt.Errorf("grant entitlement: %w", ErrNotFound)
		case "23514":
			return fmt.Errorf("grant entitlement: %w", ErrInvalidInput)
		}
	}
	return fmt.Errorf("grant entitlement: %w", err)
}

func auditEntitlementChange(
	ctx context.Context,
	transaction pgx.Tx,
	actorID string,
	subject EntitlementSubject,
	action string,
	feature string,
) error {
	if subject.OrganizationID == "" {
		// The current audit log is organization-scoped. User entitlement changes
		// need an instance-wide audit log before they can be recorded usefully.
		return nil
	}
	return insertAudit(
		ctx,
		transaction,
		actorID,
		subject.OrganizationID,
		"",
		action,
		"entitlement",
		feature,
	)
}
