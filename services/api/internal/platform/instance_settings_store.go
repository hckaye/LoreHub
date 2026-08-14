package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const hostedLoreServerEnabledSettingKey = "hosted_lore_server_enabled"

func (store *Store) GetHostedLoreServerOverride(ctx context.Context) (*bool, error) {
	return readHostedLoreServerOverride(ctx, store.pool)
}

func (store *Store) SetHostedLoreServerOverride(
	ctx context.Context,
	actor User,
	value *bool,
) error {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin hosted Lore server setting update: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	if value == nil {
		if _, err := transaction.Exec(ctx, `
			DELETE FROM instance_settings WHERE key = $1
		`, hostedLoreServerEnabledSettingKey); err != nil {
			return fmt.Errorf("clear hosted Lore server setting: %w", err)
		}
	} else {
		encoded, err := json.Marshal(*value)
		if err != nil {
			return fmt.Errorf("encode hosted Lore server setting: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO instance_settings (key, value, updated_by, updated_at)
			VALUES ($1, $2::jsonb, NULLIF($3, ''), now())
			ON CONFLICT (key) DO UPDATE SET
				value = EXCLUDED.value,
				updated_by = EXCLUDED.updated_by,
				updated_at = EXCLUDED.updated_at
		`, hostedLoreServerEnabledSettingKey, encoded, actor.ID); err != nil {
			return fmt.Errorf("set hosted Lore server setting: %w", err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit hosted Lore server setting update: %w", err)
	}
	return nil
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
