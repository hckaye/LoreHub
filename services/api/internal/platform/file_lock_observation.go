package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type loreFileLockEvent struct {
	ActorID        string    `json:"actorId"`
	RepositoryID   string    `json:"repositoryId"`
	LoreRepository string    `json:"loreRepositoryId"`
	BranchID       string    `json:"branchId"`
	Path           string    `json:"path"`
	OwnerID        string    `json:"ownerId"`
	LockedAt       time.Time `json:"lockedAt"`
}

func (store *Store) RecordLoreFileLockAcquisition(
	ctx context.Context,
	actorID string,
	loreRepositoryID string,
	branchID string,
	filePath string,
	ownerID string,
	lockedAt time.Time,
) error {
	return store.recordLoreFileLock(
		ctx, actorID, loreRepositoryID, branchID, filePath, ownerID, lockedAt, "acquired",
	)
}

func (store *Store) RecordLoreFileLockRelease(
	ctx context.Context,
	actorID string,
	loreRepositoryID string,
	branchID string,
	filePath string,
	ownerID string,
	lockedAt time.Time,
) error {
	return store.recordLoreFileLock(
		ctx, actorID, loreRepositoryID, branchID, filePath, ownerID, lockedAt, "released",
	)
}

func (store *Store) recordLoreFileLock(
	ctx context.Context,
	actorID string,
	loreRepositoryID string,
	branchID string,
	filePath string,
	ownerID string,
	lockedAt time.Time,
	result string,
) error {
	if actorID == "" || loreRepositoryID == "" || branchID == "" || filePath == "" ||
		ownerID == "" || lockedAt.IsZero() || (result != "acquired" && result != "released") {
		return errors.New("the Lore file lock observation is incomplete")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin Lore file lock observation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	repositoryID, organizationID, err := loreObservationRepository(ctx, tx, actorID, loreRepositoryID)
	if err != nil {
		return err
	}
	event := loreFileLockEvent{
		ActorID: actorID, RepositoryID: repositoryID, LoreRepository: loreRepositoryID,
		BranchID: branchID, Path: filePath, OwnerID: ownerID, LockedAt: lockedAt.UTC(),
	}
	inserted, err := insertLoreFileLockOutbox(ctx, tx, result, event)
	if err != nil {
		return err
	}
	if inserted {
		action := "file_lock.acquire"
		if result == "released" {
			action = "file_lock.release"
		}
		details := map[string]any{
			"branchId": branchID, "path": filePath,
			"ownerId": ownerID, "lockedAt": lockedAt.UTC(),
		}
		if err := insertAuditDetails(
			ctx, tx, actorID, organizationID, repositoryID,
			action, "lore_file", filePath, details,
		); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Lore file lock observation: %w", err)
	}
	return nil
}

func insertLoreFileLockOutbox(
	ctx context.Context,
	tx pgx.Tx,
	result string,
	event loreFileLockEvent,
) (bool, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return false, fmt.Errorf("encode Lore file lock observation: %w", err)
	}
	keyBytes, err := json.Marshal([]any{
		event.LoreRepository, event.BranchID, event.Path, event.OwnerID, event.LockedAt.UTC(),
	})
	if err != nil {
		return false, fmt.Errorf("encode Lore file lock observation key: %w", err)
	}
	var insertedID string
	err = tx.QueryRow(ctx, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (topic, event_key) DO NOTHING
		RETURNING id
	`, uuid.New(), "file_lock."+result, string(keyBytes), payload).Scan(&insertedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("record Lore file lock outbox event: %w", err)
	}
	return true, nil
}
