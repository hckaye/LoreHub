package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRepositoryTopicDatabaseConstraints(t *testing.T) {
	pool, _ := identityIntegrationStore(t)
	ctx := context.Background()
	user := platformTestUser("topic-owner-" + uuid.NewString())
	organizationID := uuid.NewString()
	repositoryID := uuid.NewString()
	mustIdentityExec(t, pool, `
		INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
	`, user.ID, user.Username, user.DisplayName)
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Topic organization', 'public', $3)
	`, organizationID, "topic-org-"+uuid.NewString(), user.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Topic repository', 'public', $4, $5, 'main', $6)
	`, repositoryID, organizationID, "topic-repo-"+uuid.NewString(), canonicalTestLoreID(repositoryID),
		"lore://"+repositoryID, user.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, organizationID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user.ID)
	})
	_, err := pool.Exec(ctx, `
		INSERT INTO repository_topics (repository_id, topic, created_by) VALUES ($1, 'bad_topic', $2)
	`, repositoryID, user.ID)
	assertTopicConstraintViolation(t, err)
	for index := 0; index < maxRepositoryTopics; index++ {
		mustIdentityExec(t, pool, `
			INSERT INTO repository_topics (repository_id, topic, created_by) VALUES ($1, $2, $3)
		`, repositoryID, fmt.Sprintf("topic-%d", index), user.ID)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO repository_topics (repository_id, topic, created_by) VALUES ($1, 'overflow', $2)
	`, repositoryID, user.ID)
	assertTopicConstraintViolation(t, err)
}

func assertTopicConstraintViolation(t *testing.T, err error) {
	t.Helper()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23514" {
		t.Fatalf("topic constraint error = %v", err)
	}
}
