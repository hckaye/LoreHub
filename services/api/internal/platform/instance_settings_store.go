package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const (
	hostedLoreServerEnabledSettingKey        = "hosted_lore_server_enabled"
	maxOrganizationsPerUserSettingKey        = "max_organizations_per_user"
	maxRepositoriesPerOrganizationSettingKey = "max_repositories_per_organization"
	maxRepositorySizeBytesSettingKey         = "max_repository_size_bytes"
)

func (store *Store) GetHostedLoreServerOverride(ctx context.Context) (*bool, error) {
	return readHostedLoreServerOverride(ctx, store.pool)
}

func (store *Store) SetHostedLoreServerOverride(
	ctx context.Context,
	actor User,
	value *bool,
) error {
	var encoded []byte
	if value != nil {
		var err error
		encoded, err = json.Marshal(*value)
		if err != nil {
			return fmt.Errorf("encode hosted Lore server setting: %w", err)
		}
	}
	return store.writeInstanceSetting(ctx, actor, hostedLoreServerEnabledSettingKey, encoded, "hosted Lore server")
}

func (store *Store) HostedLoreServerEnabled(ctx context.Context, defaultEnabled bool) (bool, error) {
	override, err := store.GetHostedLoreServerOverride(ctx)
	if err != nil {
		return false, err
	}
	if override == nil {
		return defaultEnabled, nil
	}
	return *override, nil
}

func (store *Store) GetMaxOrganizationsPerUserOverride(ctx context.Context) (*int64, error) {
	return readMaxOrganizationsPerUserOverride(ctx, store.pool)
}

func (store *Store) SetMaxOrganizationsPerUserOverride(
	ctx context.Context,
	actor User,
	value *int64,
) error {
	if err := requireNonNegativeOverride(value, "max organizations per user"); err != nil {
		return err
	}
	return store.writeInt64Setting(ctx, actor, maxOrganizationsPerUserSettingKey, value, "max organizations per user")
}

func (store *Store) GetMaxRepositoriesPerOrganizationOverride(ctx context.Context) (*int64, error) {
	return readMaxRepositoriesPerOrganizationOverride(ctx, store.pool)
}

func (store *Store) SetMaxRepositoriesPerOrganizationOverride(
	ctx context.Context,
	actor User,
	value *int64,
) error {
	if err := requireNonNegativeOverride(value, "max repositories per organization"); err != nil {
		return err
	}
	return store.writeInt64Setting(
		ctx, actor, maxRepositoriesPerOrganizationSettingKey, value, "max repositories per organization",
	)
}

func (store *Store) GetMaxRepositorySizeBytesOverride(ctx context.Context) (*int64, error) {
	return readMaxRepositorySizeBytesOverride(ctx, store.pool)
}

func (store *Store) SetMaxRepositorySizeBytesOverride(
	ctx context.Context,
	actor User,
	value *int64,
) error {
	if err := requireNonNegativeOverride(value, "max repository size bytes"); err != nil {
		return err
	}
	return store.writeInt64Setting(ctx, actor, maxRepositorySizeBytesSettingKey, value, "max repository size bytes")
}

func (store *Store) EffectiveMaxRepositorySizeBytes(ctx context.Context) (int64, error) {
	return store.maxRepositorySizeBytesLimit(ctx, store.pool)
}

func (store *Store) writeInt64Setting(
	ctx context.Context,
	actor User,
	key string,
	value *int64,
	label string,
) error {
	var encoded []byte
	if value != nil {
		var err error
		encoded, err = json.Marshal(*value)
		if err != nil {
			return fmt.Errorf("encode %s setting: %w", label, err)
		}
	}
	return store.writeInstanceSetting(ctx, actor, key, encoded, label)
}

func (store *Store) writeInstanceSetting(
	ctx context.Context,
	actor User,
	key string,
	encoded []byte,
	label string,
) error {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin %s setting update: %w", label, err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	if encoded == nil {
		if _, err := transaction.Exec(ctx, `
			DELETE FROM instance_settings WHERE key = $1
		`, key); err != nil {
			return fmt.Errorf("clear %s setting: %w", label, err)
		}
	} else {
		if _, err := transaction.Exec(ctx, `
			INSERT INTO instance_settings (key, value, updated_by, updated_at)
			VALUES ($1, $2::jsonb, NULLIF($3, ''), now())
			ON CONFLICT (key) DO UPDATE SET
				value = EXCLUDED.value,
				updated_by = EXCLUDED.updated_by,
				updated_at = EXCLUDED.updated_at
		`, key, encoded, actor.ID); err != nil {
			return fmt.Errorf("set %s setting: %w", label, err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s setting update: %w", label, err)
	}
	return nil
}

type instanceSettingsQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readHostedLoreServerOverride(
	ctx context.Context,
	query instanceSettingsQuery,
) (*bool, error) {
	var encoded string
	err := query.QueryRow(ctx, `
		SELECT value::text
		FROM instance_settings
		WHERE key = $1
	`, hostedLoreServerEnabledSettingKey).Scan(&encoded)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read hosted Lore server setting: %w", err)
	}
	var value *bool
	if err := json.Unmarshal([]byte(encoded), &value); err != nil || value == nil {
		if err == nil {
			err = errors.New("value must be a boolean")
		}
		return nil, fmt.Errorf("decode hosted Lore server setting: %w", err)
	}
	return value, nil
}

func readMaxOrganizationsPerUserOverride(
	ctx context.Context,
	query instanceSettingsQuery,
) (*int64, error) {
	return readInt64Override(ctx, query, maxOrganizationsPerUserSettingKey, "max organizations per user")
}

func readMaxRepositoriesPerOrganizationOverride(
	ctx context.Context,
	query instanceSettingsQuery,
) (*int64, error) {
	return readInt64Override(
		ctx, query, maxRepositoriesPerOrganizationSettingKey, "max repositories per organization",
	)
}

func readMaxRepositorySizeBytesOverride(
	ctx context.Context,
	query instanceSettingsQuery,
) (*int64, error) {
	return readInt64Override(ctx, query, maxRepositorySizeBytesSettingKey, "max repository size bytes")
}

func readInt64Override(
	ctx context.Context,
	query instanceSettingsQuery,
	key string,
	label string,
) (*int64, error) {
	var encoded string
	err := query.QueryRow(ctx, `
		SELECT value::text
		FROM instance_settings
		WHERE key = $1
	`, key).Scan(&encoded)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s setting: %w", label, err)
	}
	var value *int64
	if err := json.Unmarshal([]byte(encoded), &value); err != nil || value == nil {
		if err == nil {
			err = errors.New("value must be an integer")
		}
		return nil, fmt.Errorf("decode %s setting: %w", label, err)
	}
	if *value < 0 {
		return nil, fmt.Errorf("decode %s setting: value must be non-negative", label)
	}
	return value, nil
}

func requireNonNegativeOverride(value *int64, label string) error {
	if value != nil && *value < 0 {
		return fmt.Errorf("%w: %s must be non-negative", ErrInvalidInput, label)
	}
	return nil
}

func effectiveInt64Override(override *int64, fallback int64) int64 {
	if override != nil {
		return *override
	}
	return fallback
}
