package platform

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/lorehub/lorehub/services/api/internal/auth"
)

var loreServerCertificateSerialPattern = regexp.MustCompile(`^[0-9a-f]{1,64}$`)

func (store *Store) RecordServerCertificate(
	ctx context.Context,
	serverID string,
	serial string,
	issuedAt time.Time,
	expiresAt time.Time,
) error {
	if _, err := uuid.Parse(serverID); err != nil ||
		!loreServerCertificateSerialPattern.MatchString(serial) || issuedAt.IsZero() ||
		expiresAt.IsZero() || !expiresAt.After(issuedAt) {
		return ErrInvalidInput
	}
	command, err := store.pool.Exec(ctx, `
		UPDATE lore_servers
		SET hook_certificate_serial = $2, hook_certificate_issued_at = $3,
		    hook_certificate_expires_at = $4, updated_at = now()
		WHERE id = $1 AND status = 'active' AND revoked_at IS NULL
	`, serverID, serial, issuedAt.UTC(), expiresAt.UTC())
	if err != nil {
		return fmt.Errorf("record Lore server certificate: %w", err)
	}
	if command.RowsAffected() != 1 {
		return auth.ErrInvalidLoreServerCredential
	}
	return nil
}

func (store *Store) ActiveLoreServerForHook(ctx context.Context, serverID string) (LoreServer, error) {
	if parsed, err := uuid.Parse(serverID); err != nil || parsed.String() != serverID {
		return LoreServer{}, ErrNotFound
	}
	server, err := scanLoreServer(store.pool.QueryRow(ctx, loreServerSelect+`
		WHERE server.id = $1 AND server.status = 'active' AND server.revoked_at IS NULL
	`, serverID))
	if errors.Is(err, pgx.ErrNoRows) {
		return LoreServer{}, ErrNotFound
	}
	if err != nil {
		return LoreServer{}, fmt.Errorf("resolve Lore server hook identity: %w", err)
	}
	return server, nil
}

func (store *Store) LoreServerOwnsRepository(
	ctx context.Context,
	serverID string,
	loreRepositoryID string,
) (bool, error) {
	if parsed, err := uuid.Parse(serverID); err != nil || parsed.String() != serverID ||
		!partitionPattern.MatchString(loreRepositoryID) {
		return false, ErrInvalidInput
	}
	var owns bool
	err := store.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM repositories repository
			JOIN lore_servers server ON server.id = $2
			WHERE repository.lore_repository_id = $1
			  AND server.status = 'active' AND server.revoked_at IS NULL
			  AND (
				  server.id = repository.lore_server_id
				  OR EXISTS (
					  SELECT 1
					  FROM repository_migrations migration
					  WHERE migration.repository_id = repository.id
						AND migration.to_server_id = server.id
						AND migration.state IN ('pending', 'mirroring', 'repointing')
				  )
				)
		)
	`, loreRepositoryID, serverID).Scan(&owns)
	if err != nil {
		return false, fmt.Errorf("verify Lore server repository assignment: %w", err)
	}
	return owns, nil
}
