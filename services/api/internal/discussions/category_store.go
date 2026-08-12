package discussions

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (store *store) CreateCategory(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	input CategoryInput,
) (Category, error) {
	input, err := normalizeCategoryInput(input)
	if err != nil {
		return Category{}, err
	}
	tx, err := store.beginAuthorized(ctx, actor, repository, permissionAdmin, "create discussion category")
	if err != nil {
		return Category{}, err
	}
	defer rollback(ctx, tx)
	category := Category{
		ID: uuid.NewString(), Slug: input.Slug, Name: input.Name,
		Description: input.Description, Format: input.Format,
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO discussion_categories (
			id, repository_id, slug, name, description, format, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at
	`, category.ID, repository.ID, category.Slug, category.Name,
		category.Description, category.Format, actor.ID).Scan(&category.CreatedAt, &category.UpdatedAt)
	if err != nil {
		return Category{}, translateStoreError("create discussion category", err)
	}
	if err := recordMutation(
		ctx,
		tx,
		actor.ID,
		repository,
		"discussion.category.created",
		"discussion_category",
		category.ID,
		map[string]any{"slug": category.Slug},
	); err != nil {
		return Category{}, err
	}
	if err := commit(ctx, tx, "create discussion category"); err != nil {
		return Category{}, err
	}
	return category, nil
}

func (store *store) UpdateCategory(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	slug string,
	input CategoryInput,
) (Category, error) {
	input, err := normalizeCategoryInput(input)
	if err != nil {
		return Category{}, err
	}
	slug = normalizeCategorySlug(slug)
	if !categorySlugPattern.MatchString(slug) {
		return Category{}, platform.ErrNotFound
	}
	tx, err := store.beginAuthorized(ctx, actor, repository, permissionAdmin, "update discussion category")
	if err != nil {
		return Category{}, err
	}
	defer rollback(ctx, tx)
	var category Category
	err = tx.QueryRow(ctx, `
		UPDATE discussion_categories
		SET slug = $3, name = $4, description = $5, format = $6, updated_at = now()
		WHERE repository_id = $1 AND lower(slug) = lower($2)
		RETURNING id::text, slug, name, description, format, created_at, updated_at
	`, repository.ID, slug, input.Slug, input.Name, input.Description, input.Format).Scan(
		&category.ID,
		&category.Slug,
		&category.Name,
		&category.Description,
		&category.Format,
		&category.CreatedAt,
		&category.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Category{}, platform.ErrNotFound
	}
	if err != nil {
		return Category{}, translateStoreError("update discussion category", err)
	}
	if input.Format != "question" {
		if _, err := tx.Exec(ctx, `
			UPDATE discussions
			SET answered_comment_id = NULL, updated_at = now()
			WHERE repository_id = $1 AND category_id = $2 AND answered_comment_id IS NOT NULL
		`, repository.ID, category.ID); err != nil {
			return Category{}, fmt.Errorf("clear answers after discussion category update: %w", err)
		}
	}
	if err := recordMutation(
		ctx,
		tx,
		actor.ID,
		repository,
		"discussion.category.updated",
		"discussion_category",
		category.ID,
		map[string]any{"slug": category.Slug},
	); err != nil {
		return Category{}, err
	}
	if err := commit(ctx, tx, "update discussion category"); err != nil {
		return Category{}, err
	}
	return category, nil
}

func (store *store) DeleteCategory(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	slug string,
) error {
	slug = normalizeCategorySlug(slug)
	if !categorySlugPattern.MatchString(slug) {
		return platform.ErrNotFound
	}
	tx, err := store.beginAuthorized(ctx, actor, repository, permissionAdmin, "delete discussion category")
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	var categoryID string
	err = tx.QueryRow(ctx, `
		DELETE FROM discussion_categories
		WHERE repository_id = $1 AND lower(slug) = lower($2)
		RETURNING id::text
	`, repository.ID, slug).Scan(&categoryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.ErrNotFound
	}
	if err != nil {
		return translateStoreError("delete discussion category", err)
	}
	if err := recordMutation(
		ctx,
		tx,
		actor.ID,
		repository,
		"discussion.category.deleted",
		"discussion_category",
		categoryID,
		map[string]any{"slug": slug},
	); err != nil {
		return err
	}
	if err := commit(ctx, tx, "delete discussion category"); err != nil {
		return fmt.Errorf("delete discussion category: %w", err)
	}
	return nil
}
