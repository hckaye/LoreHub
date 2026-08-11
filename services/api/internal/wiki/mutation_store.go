package wiki

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
) (Page, error) {
	normalized, err := normalizeCreate(input)
	if err != nil {
		return Page{}, err
	}
	tx, err := store.beginWrite(ctx, actor, repository, "create wiki page")
	if err != nil {
		return Page{}, err
	}
	defer rollback(ctx, tx)
	page := Page{PageSummary: PageSummary{
		ID: uuid.NewString(), Slug: normalized.Slug, Title: normalized.Title, Version: 1,
		CreatedBy: actor.Username, UpdatedBy: actor.Username,
	}, Body: normalized.Body, ViewerCanWrite: true}
	err = tx.QueryRow(ctx, `
		INSERT INTO repository_wiki_pages (
			id, repository_id, slug, title, body, version, created_by, updated_by
		) VALUES ($1, $2, $3, $4, $5, 1, $6, $6)
		RETURNING created_at, updated_at
	`, page.ID, repository.ID, page.Slug, page.Title, page.Body, actor.ID).Scan(
		&page.CreatedAt, &page.UpdatedAt,
	)
	if err != nil {
		return Page{}, storeError("create wiki page", err)
	}
	if err := insertVersion(ctx, tx, repository.ID, page, normalized.EditSummary, actor.ID); err != nil {
		return Page{}, err
	}
	if err := recordMutation(ctx, tx, actor.ID, repository, "wiki_page.create", "wiki.created", page); err != nil {
		return Page{}, err
	}
	if err := commit(ctx, tx, "create wiki page"); err != nil {
		return Page{}, err
	}
	return page, nil
}

func (store *store) Update(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	slug string,
	input UpdateInput,
) (Page, error) {
	if input.ExpectedVersion < 1 {
		return Page{}, invalid("expected version must be positive")
	}
	tx, err := store.beginWrite(ctx, actor, repository, "update wiki page")
	if err != nil {
		return Page{}, err
	}
	defer rollback(ctx, tx)
	current, err := lockPage(ctx, tx, repository.ID, slug)
	if err != nil {
		return Page{}, err
	}
	if current.Version != input.ExpectedVersion {
		return Page{}, platform.ErrConflict
	}
	normalized, err := normalizeUpdate(current, input)
	if err != nil {
		return Page{}, err
	}
	now := time.Now().UTC()
	current.Slug = normalized.Slug
	current.Title = normalized.Title
	current.Body = normalized.Body
	current.Version++
	current.UpdatedBy = actor.Username
	current.UpdatedAt = now
	current.ViewerCanWrite = true
	_, err = tx.Exec(ctx, `
		UPDATE repository_wiki_pages
		SET slug = $3, title = $4, body = $5, version = $6,
		    updated_by = $7, updated_at = $8
		WHERE id = $1 AND repository_id = $2 AND archived_at IS NULL
	`, current.ID, repository.ID, current.Slug, current.Title, current.Body,
		current.Version, actor.ID, now)
	if err != nil {
		return Page{}, storeError("update wiki page", err)
	}
	if err := insertVersion(ctx, tx, repository.ID, current, normalized.EditSummary, actor.ID); err != nil {
		return Page{}, err
	}
	if err := recordMutation(
		ctx, tx, actor.ID, repository, "wiki_page.update", "wiki.updated", current,
	); err != nil {
		return Page{}, err
	}
	if err := commit(ctx, tx, "update wiki page"); err != nil {
		return Page{}, err
	}
	return current, nil
}

func (store *store) Delete(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	slug string,
	expectedVersion int,
) error {
	if expectedVersion < 1 {
		return invalid("expected version must be positive")
	}
	tx, err := store.beginWrite(ctx, actor, repository, "delete wiki page")
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	page, err := lockPage(ctx, tx, repository.ID, slug)
	if err != nil {
		return err
	}
	if page.Version != expectedVersion {
		return platform.ErrConflict
	}
	_, err = tx.Exec(ctx, `
		UPDATE repository_wiki_pages
		SET archived_at = now(), archived_by = $3, updated_by = $3, updated_at = now()
		WHERE id = $1 AND repository_id = $2 AND archived_at IS NULL
	`, page.ID, repository.ID, actor.ID)
	if err != nil {
		return fmt.Errorf("archive wiki page: %w", err)
	}
	if err := recordMutation(ctx, tx, actor.ID, repository, "wiki_page.delete", "wiki.deleted", page); err != nil {
		return err
	}
	return commit(ctx, tx, "delete wiki page")
}

func lockPage(ctx context.Context, tx pgx.Tx, repositoryID string, slug string) (Page, error) {
	page, err := scanPage(tx.QueryRow(ctx, pageQuery+`
		WHERE page.repository_id = $1 AND page.slug = $2 AND page.archived_at IS NULL
		FOR UPDATE OF page
	`, repositoryID, slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return Page{}, platform.ErrNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("lock wiki page: %w", err)
	}
	return page, nil
}

func insertVersion(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	page Page,
	editSummary string,
	editorID string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO repository_wiki_page_versions (
			page_id, repository_id, version, slug, title, body, edit_summary, edited_by, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, page.ID, repositoryID, page.Version, page.Slug, page.Title, page.Body,
		editSummary, editorID, page.UpdatedAt)
	if err != nil {
		return storeError("record wiki page version", err)
	}
	return nil
}

func recordMutation(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repository RepositoryRef,
	action string,
	topic string,
	page Page,
) error {
	if err := insertAudit(ctx, tx, actorID, repository, action, page.ID); err != nil {
		return err
	}
	return insertOutbox(ctx, tx, topic, repository.ID, page)
}
