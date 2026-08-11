package database

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/migrations"
)

func TestIssueAssigneeMigrationPreservesLegacyAssignment(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set; skipping PostgreSQL migration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	defer connection.Close(context.Background())
	schema := "assignee_migration_" + uuid.NewString()[:8]
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := connection.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatalf("create migration schema: %v", err)
	}
	defer func() {
		_, _ = connection.Exec(context.Background(), "SET search_path TO public")
		_, _ = connection.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE")
	}()
	if _, err := connection.Exec(ctx, "SET search_path TO "+identifier); err != nil {
		t.Fatalf("set migration search path: %v", err)
	}
	applyMigrationsBefore(t, ctx, connection, 29)

	authorID := uuid.NewString()
	assigneeID := uuid.NewString()
	organizationID := uuid.NewString()
	repositoryID := uuid.NewString()
	issueID := uuid.NewString()
	mustMigrationExec(t, ctx, connection, `
		INSERT INTO users (id, username, display_name)
		VALUES ($1, 'legacy-author', 'Legacy Author'),
		       ($2, 'legacy-assignee', 'Legacy Assignee')
	`, authorID, assigneeID)
	mustMigrationExec(t, ctx, connection, `
		INSERT INTO organizations (
			id, slug, display_name, description, visibility, created_by
		) VALUES ($1, 'legacy-org', 'Legacy Org', '', 'private', $2)
	`, organizationID, authorID)
	mustMigrationExec(t, ctx, connection, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES (
			$1, $2, 'legacy-repo', 'Legacy Repo', '', 'private',
			$3, 'https://lore.invalid/legacy', 'main', $4
		)
	`, repositoryID, organizationID, compactMigrationUUID(repositoryID), authorID)
	mustMigrationExec(t, ctx, connection, `
		INSERT INTO repository_counters (repository_id) VALUES ($1)
	`, repositoryID)
	mustMigrationExec(t, ctx, connection, `
		INSERT INTO issues (
			id, repository_id, number, title, body, author_id, assignee_id
		) VALUES ($1, $2, 1, 'Legacy issue', '', $3, $4)
	`, issueID, repositoryID, authorID, assigneeID)
	applyMigrationFile(t, ctx, connection, 29)

	var migratedUserID, assignedBy string
	if err := connection.QueryRow(ctx, `
		SELECT user_id, assigned_by FROM issue_assignees
		WHERE issue_id = $1 AND repository_id = $2
	`, issueID, repositoryID).Scan(&migratedUserID, &assignedBy); err != nil {
		t.Fatalf("read migrated assignment: %v", err)
	}
	if migratedUserID != assigneeID || assignedBy != authorID {
		t.Fatalf("migrated user = %s, assigned by = %s", migratedUserID, assignedBy)
	}
	var legacyColumnExists bool
	if err := connection.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = 'issues'
			  AND column_name = 'assignee_id'
		)
	`, schema).Scan(&legacyColumnExists); err != nil {
		t.Fatalf("inspect legacy column: %v", err)
	}
	if legacyColumnExists {
		t.Fatal("legacy assignee_id column still exists")
	}
}

func mustMigrationExec(
	t *testing.T,
	ctx context.Context,
	connection *pgx.Conn,
	query string,
	arguments ...any,
) {
	t.Helper()
	if _, err := connection.Exec(ctx, query, arguments...); err != nil {
		t.Fatalf("seed legacy assignment: %v", err)
	}
}

func applyMigrationsBefore(
	t *testing.T,
	ctx context.Context,
	connection *pgx.Conn,
	before int,
) {
	t.Helper()
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") || len(entry.Name()) < 7 {
			continue
		}
		version, err := strconv.Atoi(entry.Name()[:6])
		if err != nil {
			t.Fatalf("parse migration version for %s: %v", entry.Name(), err)
		}
		if version >= before {
			continue
		}
		contents, err := migrations.Files.ReadFile(entry.Name())
		if err != nil {
			t.Fatalf("read migration %d: %v", version, err)
		}
		if _, err := connection.Exec(ctx, string(contents)); err != nil {
			t.Fatalf("apply migration %d: %v", version, err)
		}
	}
}

func applyMigrationFile(t *testing.T, ctx context.Context, connection *pgx.Conn, version int) {
	t.Helper()
	name := fmt.Sprintf("%06d_", version)
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if len(entry.Name()) < len(name) || entry.Name()[:len(name)] != name {
			continue
		}
		contents, err := migrations.Files.ReadFile(entry.Name())
		if err != nil {
			t.Fatalf("read migration %d: %v", version, err)
		}
		if _, err := connection.Exec(ctx, string(contents)); err != nil {
			t.Fatalf("apply migration %d: %v", version, err)
		}
		return
	}
	t.Fatalf("migration %d was not found", version)
}

func compactMigrationUUID(value string) string {
	result := make([]byte, 0, 32)
	for _, character := range []byte(value) {
		if character != '-' {
			result = append(result, character)
		}
	}
	return string(result)
}
