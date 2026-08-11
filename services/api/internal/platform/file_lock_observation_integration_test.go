package platform

import (
	"context"
	"testing"
	"time"
)

func TestFileLockObservationIntegrationIsIdempotent(t *testing.T) {
	fixture := authorizationIntegrationFixture(t)
	ctx := context.Background()
	lockedAt := time.Date(2026, 8, 12, 3, 4, 5, 6000000, time.UTC)
	if err := fixture.store.RecordLoreFileLockAcquisition(
		ctx, fixture.alice.ID, fixture.loreA, "branch-id",
		"Content/Hero.uasset", fixture.alice.ID, lockedAt,
	); err != nil {
		t.Fatalf("record acquisition: %v", err)
	}
	if err := fixture.store.RecordLoreFileLockAcquisition(
		ctx, fixture.alice.ID, fixture.loreA, "branch-id",
		"Content/Hero.uasset", fixture.alice.ID, lockedAt,
	); err != nil {
		t.Fatalf("record duplicate acquisition: %v", err)
	}
	if err := fixture.store.RecordLoreFileLockRelease(
		ctx, fixture.alice.ID, fixture.loreA, "branch-id",
		"Content/Hero.uasset", fixture.alice.ID, lockedAt,
	); err != nil {
		t.Fatalf("record release: %v", err)
	}
	if err := fixture.store.RecordLoreFileLockAcquisition(
		ctx, fixture.manager.ID, fixture.loreB, "branch-id",
		"Content/Hero.uasset", fixture.alice.ID, lockedAt,
	); err != nil {
		t.Fatalf("record same lock key in another repository: %v", err)
	}
	var acquisitions, releases int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE action = 'file_lock.acquire'),
			count(*) FILTER (WHERE action = 'file_lock.release')
		FROM audit_events
		WHERE repository_id = $1 AND target_id = 'Content/Hero.uasset'
	`, fixture.repositoryA).Scan(&acquisitions, &releases); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if acquisitions != 1 || releases != 1 {
		t.Fatalf("audit counts = acquire %d, release %d", acquisitions, releases)
	}
	var secondRepositoryAcquisitions int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM audit_events
		WHERE repository_id = $1 AND action = 'file_lock.acquire'
	`, fixture.repositoryB).Scan(&secondRepositoryAcquisitions); err != nil {
		t.Fatalf("count second repository audit events: %v", err)
	}
	if secondRepositoryAcquisitions != 1 {
		t.Fatalf("second repository acquisitions = %d", secondRepositoryAcquisitions)
	}
	var path, ownerID string
	if err := fixture.pool.QueryRow(ctx, `
		SELECT details->>'path', details->>'ownerId'
		FROM audit_events
		WHERE repository_id = $1 AND action = 'file_lock.acquire'
	`, fixture.repositoryA).Scan(&path, &ownerID); err != nil {
		t.Fatalf("read audit details: %v", err)
	}
	if path != "Content/Hero.uasset" || ownerID != fixture.alice.ID {
		t.Fatalf("audit details = path %q, owner %q", path, ownerID)
	}
}
