package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (store *Store) ListOrganizationMembers(
	ctx context.Context,
	actor User,
	organizationSlug string,
) ([]OrganizationMember, error) {
	organizationID, _, err := store.organizationAccess(ctx, actor.ID, organizationSlug)
	if err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT u.id, u.username, u.display_name, om.role, om.active, om.created_at
		FROM organization_memberships om
		JOIN users u ON u.id = om.user_id
		WHERE om.organization_id = $1
		ORDER BY u.username
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list organization members: %w", err)
	}
	defer rows.Close()
	members := make([]OrganizationMember, 0)
	for rows.Next() {
		var member OrganizationMember
		if err := rows.Scan(&member.UserID, &member.Username, &member.DisplayName, &member.Role,
			&member.Active, &member.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan organization member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate organization members: %w", err)
	}
	return members, nil
}

func (store *Store) SetOrganizationMember(
	ctx context.Context,
	actor User,
	organizationSlug string,
	input SetOrganizationMemberInput,
) (OrganizationMember, error) {
	organizationID, actorRole, err := store.organizationAccess(ctx, actor.ID, organizationSlug)
	if err != nil {
		return OrganizationMember{}, err
	}
	if !canManageOrganization(actorRole) {
		return OrganizationMember{}, ErrForbidden
	}
	input.Username = strings.ToLower(strings.TrimSpace(input.Username))
	input.Role = strings.ToLower(strings.TrimSpace(input.Role))
	if input.Username == "" || (input.Role != "owner" && input.Role != "maintainer" && input.Role != "member") {
		return OrganizationMember{}, errors.New("invalid organization member")
	}
	if input.Role == "owner" && actorRole != "owner" {
		return OrganizationMember{}, ErrForbidden
	}
	var userID string
	err = store.pool.QueryRow(ctx, `
		SELECT id
		FROM users
		WHERE username = $1 AND status = 'active'
	`, input.Username).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrganizationMember{}, ErrNotFound
	}
	if err != nil {
		return OrganizationMember{}, fmt.Errorf("find organization member: %w", err)
	}

	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return OrganizationMember{}, fmt.Errorf("begin organization member transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if input.Role != "owner" || !input.Active {
		rows, err := transaction.Query(ctx, `
			SELECT user_id
			FROM organization_memberships
			WHERE organization_id = $1 AND role = 'owner' AND active
			ORDER BY user_id
			FOR UPDATE
		`, organizationID)
		if err != nil {
			return OrganizationMember{}, fmt.Errorf("lock organization owners: %w", err)
		}
		ownerCount := 0
		for rows.Next() {
			var ownerID string
			if err := rows.Scan(&ownerID); err != nil {
				rows.Close()
				return OrganizationMember{}, fmt.Errorf("scan organization owner: %w", err)
			}
			if ownerID != userID {
				ownerCount++
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return OrganizationMember{}, fmt.Errorf("iterate organization owners: %w", err)
		}
		rows.Close()
		var currentRole string
		var currentActive bool
		err = transaction.QueryRow(ctx, `
			SELECT role, active
			FROM organization_memberships
			WHERE organization_id = $1 AND user_id = $2
			FOR UPDATE
		`, organizationID, userID).Scan(&currentRole, &currentActive)
		if err == nil && currentRole == "owner" && currentActive && ownerCount == 0 {
			return OrganizationMember{}, fmt.Errorf("%w: an organization must keep one active owner", ErrConflict)
		}
		if err == nil && currentRole == "owner" && currentActive && actorRole != "owner" {
			return OrganizationMember{}, ErrForbidden
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return OrganizationMember{}, fmt.Errorf("lock organization membership: %w", err)
		}
	}
	var member OrganizationMember
	err = transaction.QueryRow(ctx, `
		INSERT INTO organization_memberships (organization_id, user_id, role, active)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (organization_id, user_id) DO UPDATE
		SET role = EXCLUDED.role, active = EXCLUDED.active
		RETURNING user_id, role, active, created_at
	`, organizationID, userID, input.Role, input.Active).Scan(
		&member.UserID, &member.Role, &member.Active, &member.CreatedAt)
	if err != nil {
		return OrganizationMember{}, fmt.Errorf("set organization member: %w", err)
	}
	if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, "", "organization.member.set",
		"user", userID, map[string]any{"role": input.Role, "active": input.Active}); err != nil {
		return OrganizationMember{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return OrganizationMember{}, fmt.Errorf("commit organization member transaction: %w", err)
	}
	member.Username = input.Username
	return store.organizationMember(ctx, organizationID, userID)
}

func (store *Store) organizationMember(
	ctx context.Context,
	organizationID string,
	userID string,
) (OrganizationMember, error) {
	var member OrganizationMember
	err := store.pool.QueryRow(ctx, `
		SELECT u.id, u.username, u.display_name, om.role, om.active, om.created_at
		FROM organization_memberships om
		JOIN users u ON u.id = om.user_id
		WHERE om.organization_id = $1 AND om.user_id = $2
	`, organizationID, userID).Scan(&member.UserID, &member.Username, &member.DisplayName, &member.Role,
		&member.Active, &member.CreatedAt)
	if err != nil {
		return OrganizationMember{}, fmt.Errorf("read organization member: %w", err)
	}
	return member, nil
}
