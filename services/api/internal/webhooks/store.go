package webhooks

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool   *pgxpool.Pool
	box    *SecretBox
	target *TargetPolicy
}

func NewStore(pool *pgxpool.Pool, box *SecretBox, target *TargetPolicy) (*Store, error) {
	if pool == nil {
		return nil, errors.New("webhook PostgreSQL pool is required")
	}
	if box == nil || box.aead == nil {
		return nil, errors.New("webhook encryption is required")
	}
	if target == nil || target.resolver == nil {
		return nil, errors.New("webhook target policy is required")
	}
	return &Store{pool: pool, box: box, target: target}, nil
}

func translateStoreError(operation string, err error) error {
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.Code == "23505" {
		return fmt.Errorf("%s: %w", operation, ErrConflict)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
