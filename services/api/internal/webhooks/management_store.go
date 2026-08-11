package webhooks

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (store *Store) List(
	ctx context.Context,
	actor platform.User,
	owner string,
	repository string,
) ([]Webhook, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin webhook list: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	reference, err := managedRepository(ctx, tx, actor.ID, owner, repository, false)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT id, url, events, active, created_at, updated_at
		FROM repository_webhooks
		WHERE repository_id = $1
		ORDER BY created_at, id
	`, reference.ID)
	if err != nil {
		return nil, fmt.Errorf("list repository webhooks: %w", err)
	}
	defer rows.Close()
	items := make([]Webhook, 0)
	for rows.Next() {
		item, scanErr := scanWebhook(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repository webhooks: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit webhook list: %w", err)
	}
	return items, nil
}

func (store *Store) Create(
	ctx context.Context,
	actor platform.User,
	owner string,
	repository string,
	input CreateInput,
) (Webhook, error) {
	events, err := normalizeEvents(input.Events)
	if err != nil {
		return Webhook{}, err
	}
	if err := validateSecret(input.Secret); err != nil {
		return Webhook{}, err
	}
	active := true
	if input.Active != nil {
		active = *input.Active
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Webhook{}, fmt.Errorf("begin webhook creation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	reference, err := managedRepository(ctx, tx, actor.ID, owner, repository, true)
	if err != nil {
		return Webhook{}, err
	}
	targetURL, err := store.target.Validate(ctx, input.URL)
	if err != nil {
		return Webhook{}, err
	}
	webhookID := uuid.NewString()
	ciphertext, nonce, keyID, err := store.box.Seal(webhookID, reference.ID, input.Secret)
	if err != nil {
		return Webhook{}, err
	}
	defer clear(ciphertext)
	var webhook Webhook
	err = tx.QueryRow(ctx, `
		INSERT INTO repository_webhooks (
			id, repository_id, url, events, active, secret_ciphertext,
			secret_nonce, secret_key_id, created_by, updated_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
		RETURNING id, url, events, active, created_at, updated_at
	`, webhookID, reference.ID, targetURL, events, active, ciphertext, nonce, keyID, actor.ID).Scan(
		&webhook.ID, &webhook.URL, &webhook.Events, &webhook.Active,
		&webhook.CreatedAt, &webhook.UpdatedAt,
	)
	if err != nil {
		return Webhook{}, translateStoreError("create repository webhook", err)
	}
	webhook.SecretConfigured = true
	if err := recordAudit(ctx, tx, actor.ID, reference, "webhook.create", webhook.ID); err != nil {
		return Webhook{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Webhook{}, fmt.Errorf("commit webhook creation: %w", err)
	}
	return webhook, nil
}

func (store *Store) Update(
	ctx context.Context,
	actor platform.User,
	owner string,
	repository string,
	webhookID string,
	input UpdateInput,
) (Webhook, error) {
	if _, err := uuid.Parse(webhookID); err != nil {
		return Webhook{}, invalid("webhook ID is invalid")
	}
	var events *[]string
	if input.Events != nil {
		normalized, err := normalizeEvents(*input.Events)
		if err != nil {
			return Webhook{}, err
		}
		events = &normalized
	}
	if input.Secret != nil {
		if err := validateSecret(*input.Secret); err != nil {
			return Webhook{}, err
		}
	}
	if input.URL == nil && events == nil && input.Active == nil && input.Secret == nil {
		return Webhook{}, invalid("at least one webhook field is required")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Webhook{}, fmt.Errorf("begin webhook update: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	reference, err := managedRepository(ctx, tx, actor.ID, owner, repository, true)
	if err != nil {
		return Webhook{}, err
	}
	var targetURL *string
	if input.URL != nil {
		validated, validateErr := store.target.Validate(ctx, *input.URL)
		if validateErr != nil {
			return Webhook{}, validateErr
		}
		targetURL = &validated
	}
	var ciphertext, nonce []byte
	var keyID *string
	if input.Secret != nil {
		sealed, sealedNonce, sealedKeyID, sealErr := store.box.Seal(webhookID, reference.ID, *input.Secret)
		if sealErr != nil {
			return Webhook{}, sealErr
		}
		ciphertext, nonce, keyID = sealed, sealedNonce, &sealedKeyID
		defer clear(ciphertext)
	}
	var webhook Webhook
	err = tx.QueryRow(ctx, `
		UPDATE repository_webhooks
		SET url = COALESCE($3, url), events = COALESCE($4, events),
		    active = COALESCE($5, active), secret_ciphertext = COALESCE($6, secret_ciphertext),
		    secret_nonce = COALESCE($7, secret_nonce), secret_key_id = COALESCE($8, secret_key_id),
		    updated_by = $9, updated_at = now()
		WHERE id = $1 AND repository_id = $2
		RETURNING id, url, events, active, created_at, updated_at
	`, webhookID, reference.ID, targetURL, events, input.Active, ciphertext, nonce, keyID, actor.ID).Scan(
		&webhook.ID, &webhook.URL, &webhook.Events, &webhook.Active,
		&webhook.CreatedAt, &webhook.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Webhook{}, ErrNotFound
	}
	if err != nil {
		return Webhook{}, translateStoreError("update repository webhook", err)
	}
	webhook.SecretConfigured = true
	if err := recordAudit(ctx, tx, actor.ID, reference, "webhook.update", webhook.ID); err != nil {
		return Webhook{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Webhook{}, fmt.Errorf("commit webhook update: %w", err)
	}
	return webhook, nil
}

func (store *Store) Delete(
	ctx context.Context,
	actor platform.User,
	owner string,
	repository string,
	webhookID string,
) error {
	if _, err := uuid.Parse(webhookID); err != nil {
		return invalid("webhook ID is invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin webhook deletion: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	reference, err := managedRepository(ctx, tx, actor.ID, owner, repository, true)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM repository_webhooks WHERE id = $1 AND repository_id = $2
	`, webhookID, reference.ID)
	if err != nil {
		return fmt.Errorf("delete repository webhook: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	if err := recordAudit(ctx, tx, actor.ID, reference, "webhook.delete", webhookID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit webhook deletion: %w", err)
	}
	return nil
}

func managedRepository(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	owner string,
	repository string,
	lock bool,
) (repositoryRef, error) {
	lockClause := ""
	if lock {
		lockClause = " FOR SHARE OF r, o"
	}
	var reference repositoryRef
	err := tx.QueryRow(ctx, `
		SELECT r.id, o.id, o.slug, r.slug
		FROM repositories r
		JOIN organizations o ON o.id = r.organization_id AND o.active
		JOIN users actor ON actor.id = $3 AND actor.status = 'active'
		WHERE lower(o.slug) = lower($1) AND lower(r.slug) = lower($2)
		  AND r.lifecycle_state = 'active' AND r.archived_at IS NULL
		  AND (
		    EXISTS (
		        SELECT 1 FROM organization_memberships membership
		        WHERE membership.organization_id = o.id AND membership.user_id = actor.id
		          AND membership.role = 'owner' AND membership.active
		    )
		    OR EXISTS (
		        SELECT 1 FROM repository_memberships membership
		        WHERE membership.repository_id = r.id AND membership.user_id = actor.id
		          AND membership.role = 'admin' AND membership.active
		    )
		    OR EXISTS (
		        SELECT 1
		        FROM team_repository_roles role
		        JOIN teams team ON team.id = role.team_id AND team.organization_id = o.id AND team.active
		        JOIN team_memberships team_member
		          ON team_member.team_id = team.id AND team_member.user_id = actor.id AND team_member.active
		        JOIN organization_memberships organization_member
		          ON organization_member.organization_id = o.id
		         AND organization_member.user_id = actor.id AND organization_member.active
		        WHERE role.repository_id = r.id AND role.role = 'admin' AND role.active
		    )
		  )
	`+lockClause, owner, repository, actorID).Scan(
		&reference.ID, &reference.OrganizationID, &reference.Owner, &reference.Slug,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return repositoryRef{}, ErrForbidden
	}
	if err != nil {
		return repositoryRef{}, fmt.Errorf("authorize repository webhook management: %w", err)
	}
	return reference, nil
}

func recordAudit(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repository repositoryRef,
	action string,
	targetID string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			id, organization_id, repository_id, actor_id, action, target_type, target_id
		) VALUES ($1, $2, $3, $4, $5, 'webhook', $6)
	`, uuid.NewString(), repository.OrganizationID, repository.ID, actorID, action, targetID)
	if err != nil {
		return fmt.Errorf("record webhook audit event: %w", err)
	}
	return nil
}

type webhookRow interface {
	Scan(...any) error
}

func scanWebhook(row webhookRow) (Webhook, error) {
	var webhook Webhook
	err := row.Scan(
		&webhook.ID, &webhook.URL, &webhook.Events, &webhook.Active,
		&webhook.CreatedAt, &webhook.UpdatedAt,
	)
	if err != nil {
		return Webhook{}, fmt.Errorf("scan repository webhook: %w", err)
	}
	webhook.SecretConfigured = true
	return webhook, nil
}
