package releases

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (store *store) Create(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	input CreateInput,
) (Release, error) {
	input, err := validateCreate(input)
	if err != nil {
		return Release{}, err
	}
	tx, err := store.beginWrite(ctx, actor, repository, "release creation")
	if err != nil {
		return Release{}, err
	}
	defer rollback(ctx, tx)

	releaseID := uuid.NewString()
	now := nowUTC()
	var publishedBy *string
	var publishedAt *time.Time
	if input.State == "published" {
		publishedBy = &actor.ID
		publishedAt = &now
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO repository_releases (
			id, repository_id, tag_name, title, notes, source_branch, revision,
			state, created_by, published_by, published_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)
	`, releaseID, repository.ID, input.TagName, input.Title, input.Notes,
		input.SourceBranch, input.Revision, input.State, actor.ID, publishedBy, publishedAt, now)
	if err != nil {
		return Release{}, constraintError("create release", err)
	}
	release, err := loadRelease(ctx, tx, repository.ID, releaseID, true)
	if err != nil {
		return Release{}, err
	}
	if err := recordReleaseMutation(
		ctx, tx, actor, repository, "release.create", "release.created", releaseID, release,
	); err != nil {
		return Release{}, err
	}
	if err := commit(ctx, tx, "release creation"); err != nil {
		return Release{}, err
	}
	release.ViewerCanWrite = true
	return release, nil
}

func (store *store) Update(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	releaseID string,
	input UpdateInput,
) (Release, error) {
	input, err := validateUpdate(input)
	if err != nil {
		return Release{}, err
	}
	tx, err := store.beginWrite(ctx, actor, repository, "release update")
	if err != nil {
		return Release{}, err
	}
	defer rollback(ctx, tx)
	if _, err := lockReleaseVersion(ctx, tx, repository.ID, releaseID, input.ExpectedVersion); err != nil {
		return Release{}, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE repository_releases
		SET title = COALESCE($1, title), notes = COALESCE($2, notes),
		    version = version + 1, updated_at = $3
		WHERE id = $4 AND repository_id = $5
	`, input.Title, input.Notes, nowUTC(), releaseID, repository.ID)
	if err != nil {
		return Release{}, constraintError("update release", err)
	}
	return store.finishReleaseMutation(
		ctx, tx, actor, repository, releaseID,
		"release.update", "release.updated", releaseID+":"+uuid.NewString(), "release update",
	)
}

func (store *store) Publish(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	releaseID string,
	expectedVersion int64,
) (Release, error) {
	if expectedVersion < 1 {
		return Release{}, invalid("expectedVersion must be positive")
	}
	tx, err := store.beginWrite(ctx, actor, repository, "release publication")
	if err != nil {
		return Release{}, err
	}
	defer rollback(ctx, tx)
	state, err := lockReleaseVersion(ctx, tx, repository.ID, releaseID, expectedVersion)
	if err != nil {
		return Release{}, err
	}
	if state == "published" {
		release, loadErr := loadRelease(ctx, tx, repository.ID, releaseID, true)
		if loadErr != nil {
			return Release{}, loadErr
		}
		if err := commit(ctx, tx, "idempotent release publication"); err != nil {
			return Release{}, err
		}
		release.ViewerCanWrite = true
		return release, nil
	}
	now := nowUTC()
	_, err = tx.Exec(ctx, `
		UPDATE repository_releases
		SET state = 'published', published_by = $1, published_at = $2,
		    version = version + 1, updated_at = $2
		WHERE id = $3 AND repository_id = $4
	`, actor.ID, now, releaseID, repository.ID)
	if err != nil {
		return Release{}, constraintError("publish release", err)
	}
	return store.finishReleaseMutation(
		ctx, tx, actor, repository, releaseID,
		"release.publish", "release.published", releaseID, "release publication",
	)
}

func (store *store) Delete(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	releaseID string,
	expectedVersion int64,
) error {
	if expectedVersion < 1 {
		return invalid("expectedVersion must be positive")
	}
	tx, err := store.beginWrite(ctx, actor, repository, "release deletion")
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	if _, err := lockReleaseVersion(ctx, tx, repository.ID, releaseID, expectedVersion); err != nil {
		return err
	}
	snapshot, err := loadRelease(ctx, tx, repository.ID, releaseID, true)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM repository_releases WHERE id = $1 AND repository_id = $2
	`, releaseID, repository.ID)
	if err != nil {
		return fmt.Errorf("delete release: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return platform.ErrNotFound
	}
	if err := insertAudit(ctx, tx, actor.ID, repository, "release.delete", "release", releaseID); err != nil {
		return err
	}
	if err := insertOutbox(ctx, tx, "release.deleted", releaseID, snapshot); err != nil {
		return err
	}
	return commit(ctx, tx, "release deletion")
}

func (store *store) AddAsset(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	releaseID string,
	input AssetInput,
) (Release, error) {
	input, err := validateAsset(input)
	if err != nil {
		return Release{}, err
	}
	tx, err := store.beginWrite(ctx, actor, repository, "release asset creation")
	if err != nil {
		return Release{}, err
	}
	defer rollback(ctx, tx)
	if _, err := lockReleaseVersion(ctx, tx, repository.ID, releaseID, input.ExpectedVersion); err != nil {
		return Release{}, err
	}
	assetID := uuid.NewString()
	now := nowUTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO release_asset_links (
			id, release_id, repository_id, name, external_url, created_by, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, assetID, releaseID, repository.ID, input.Name, input.ExternalURL, actor.ID, now)
	if err != nil {
		return Release{}, constraintError("create release asset", err)
	}
	if err := bumpRelease(ctx, tx, repository.ID, releaseID, now); err != nil {
		return Release{}, err
	}
	release, err := loadRelease(ctx, tx, repository.ID, releaseID, true)
	if err != nil {
		return Release{}, err
	}
	asset, found := findAsset(release.Assets, assetID)
	if !found {
		return Release{}, fmt.Errorf("find created release asset: %w", platform.ErrNotFound)
	}
	if err := insertAudit(ctx, tx, actor.ID, repository, "release.asset.create", "release_asset", assetID); err != nil {
		return Release{}, err
	}
	if err := insertOutbox(ctx, tx, "release.asset.created", assetID, asset); err != nil {
		return Release{}, err
	}
	if err := commit(ctx, tx, "release asset creation"); err != nil {
		return Release{}, err
	}
	release.ViewerCanWrite = true
	return release, nil
}

func (store *store) DeleteAsset(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	releaseID string,
	assetID string,
	expectedVersion int64,
) (Release, error) {
	if expectedVersion < 1 {
		return Release{}, invalid("expectedVersion must be positive")
	}
	tx, err := store.beginWrite(ctx, actor, repository, "release asset deletion")
	if err != nil {
		return Release{}, err
	}
	defer rollback(ctx, tx)
	if _, err := lockReleaseVersion(ctx, tx, repository.ID, releaseID, expectedVersion); err != nil {
		return Release{}, err
	}
	var asset AssetLink
	err = tx.QueryRow(ctx, `
		DELETE FROM release_asset_links asset
		USING users creator
		WHERE asset.id = $1 AND asset.release_id = $2 AND asset.repository_id = $3
		  AND creator.id = asset.created_by
		RETURNING asset.id, asset.name, asset.external_url, creator.username, asset.created_at
	`, assetID, releaseID, repository.ID).Scan(
		&asset.ID, &asset.Name, &asset.ExternalURL, &asset.CreatedBy, &asset.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Release{}, platform.ErrNotFound
	}
	if err != nil {
		return Release{}, fmt.Errorf("delete release asset: %w", err)
	}
	if err := bumpRelease(ctx, tx, repository.ID, releaseID, nowUTC()); err != nil {
		return Release{}, err
	}
	if err := insertAudit(ctx, tx, actor.ID, repository, "release.asset.delete", "release_asset", assetID); err != nil {
		return Release{}, err
	}
	if err := insertOutbox(ctx, tx, "release.asset.deleted", assetID, asset); err != nil {
		return Release{}, err
	}
	release, err := loadRelease(ctx, tx, repository.ID, releaseID, true)
	if err != nil {
		return Release{}, err
	}
	if err := commit(ctx, tx, "release asset deletion"); err != nil {
		return Release{}, err
	}
	release.ViewerCanWrite = true
	return release, nil
}

func lockReleaseVersion(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	releaseID string,
	expectedVersion int64,
) (string, error) {
	var version int64
	var state string
	err := tx.QueryRow(ctx, `
		SELECT version, state
		FROM repository_releases
		WHERE id = $1 AND repository_id = $2
		FOR UPDATE
	`, releaseID, repositoryID).Scan(&version, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", platform.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock release: %w", err)
	}
	if version != expectedVersion {
		return "", ErrVersionConflict
	}
	return state, nil
}

func bumpRelease(ctx context.Context, tx pgx.Tx, repositoryID, releaseID string, now time.Time) error {
	command, err := tx.Exec(ctx, `
		UPDATE repository_releases
		SET version = version + 1, updated_at = $3
		WHERE id = $1 AND repository_id = $2
	`, releaseID, repositoryID, now)
	if err != nil {
		return fmt.Errorf("update release version: %w", err)
	}
	if command.RowsAffected() != 1 {
		return platform.ErrNotFound
	}
	return nil
}

func findAsset(assets []AssetLink, assetID string) (AssetLink, bool) {
	for _, asset := range assets {
		if asset.ID == assetID {
			return asset, true
		}
	}
	return AssetLink{}, false
}

func (store *store) finishReleaseMutation(
	ctx context.Context,
	tx pgx.Tx,
	actor platform.User,
	repository RepositoryRef,
	releaseID string,
	action string,
	topic string,
	eventKey string,
	operation string,
) (Release, error) {
	release, err := loadRelease(ctx, tx, repository.ID, releaseID, true)
	if err != nil {
		return Release{}, err
	}
	if err := recordReleaseMutation(
		ctx, tx, actor, repository, action, topic, eventKey, release,
	); err != nil {
		return Release{}, err
	}
	if err := commit(ctx, tx, operation); err != nil {
		return Release{}, err
	}
	release.ViewerCanWrite = true
	return release, nil
}

func recordReleaseMutation(
	ctx context.Context,
	tx pgx.Tx,
	actor platform.User,
	repository RepositoryRef,
	action string,
	topic string,
	eventKey string,
	release Release,
) error {
	if err := insertAudit(ctx, tx, actor.ID, repository, action, "release", release.ID); err != nil {
		return err
	}
	return insertOutbox(ctx, tx, topic, eventKey, release)
}
