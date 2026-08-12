package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/database"
)

const testCIRunnerPrincipalID = "00000000-0000-4000-8000-000000000002"

func TestPostgresExecutionContextResolverIntegration(t *testing.T) {
	ctx := context.Background()
	pool := openExecutionContextTestDB(t)
	defer pool.Close()
	fixture := newActionsFixture(t, pool)
	defer fixture.cleanup(t)
	defer deleteExecutionContextAudits(t, pool, fixture.userID)
	grantExecutionContextRunner(t, pool, fixture.repositoryID, fixture.userID)
	resolver := newTestExecutionContextResolver(t, pool)
	store := NewStore(pool)
	access, err := store.RepositoryForActions(ctx, fixture.owner, fixture.repositorySlug, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertEnvironment(ctx, access, fixture.userID, "Production", EnvironmentInput{
		PreventSelfReview: true,
	}); err != nil {
		t.Fatal(err)
	}
	repository := Repository{
		ID: fixture.repositoryID, Owner: fixture.owner, Slug: fixture.repositorySlug,
		LoreURL: "lore://fixture/repository", DefaultBranch: "main",
	}
	workflow := protectedWorkflowDefinition()
	if _, err := store.ObserveBranch(ctx, repository, ObservedBranch{
		ID: "context-main", Name: "main", LatestRevision: "context-rev-1",
	}, workflow); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ObserveBranch(ctx, repository, ObservedBranch{
		ID: "context-main", Name: "main", LatestRevision: "context-rev-2",
	}, workflow); err != nil {
		t.Fatal(err)
	}
	job, err := store.ClaimJob(ctx, "context-worker", time.Minute)
	if err != nil || job == nil {
		t.Fatalf("claim execution context job: %#v, %v", job, err)
	}

	organizationScope := ExecutionContextScope{
		Kind: executionContextScopeOrganization, OrganizationID: fixture.organizationID,
	}
	repositoryScope := ExecutionContextScope{
		Kind: executionContextScopeRepository, OrganizationID: fixture.organizationID,
		RepositoryID: fixture.repositoryID,
	}
	environmentScope := ExecutionContextScope{
		Kind: executionContextScopeEnvironment, OrganizationID: fixture.organizationID,
		RepositoryID: fixture.repositoryID, Environment: "Production",
	}
	upsertExecutionContextFixture(t, resolver, fixture.userID,
		organizationScope, repositoryScope, environmentScope)

	var firstNonce []byte
	if err := pool.QueryRow(ctx, `
		SELECT nonce
		FROM actions_execution_context_entries
		WHERE repository_id = $1 AND scope_kind = 'environment'
		  AND value_kind = 'secret' AND name = 'DEPLOY_TOKEN'
	`, fixture.repositoryID).Scan(&firstNonce); err != nil {
		t.Fatal(err)
	}
	metadata, err := resolver.UpsertSecret(
		ctx, environmentScope, "DEPLOY_TOKEN", "environment-secret-updated", fixture.userID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.Secret || metadata.KeyID != "actions-test-key" || metadata.Value != "" {
		t.Fatalf("secret upsert returned incomplete metadata: %#v", metadata)
	}
	var secondNonce []byte
	var variableIsNull bool
	if err := pool.QueryRow(ctx, `
		SELECT nonce, variable_value IS NULL
		FROM actions_execution_context_entries
		WHERE repository_id = $1 AND scope_kind = 'environment'
		  AND value_kind = 'secret' AND name = 'DEPLOY_TOKEN'
	`, fixture.repositoryID).Scan(&secondNonce, &variableIsNull); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(firstNonce, secondNonce) || !variableIsNull || len(secondNonce) != resolver.aead.NonceSize() {
		t.Fatalf("secret storage did not rotate its nonce or stored plaintext: %x %x", firstNonce, secondNonce)
	}

	entries, err := resolver.ListExecutionContextEntries(ctx, fixture.userID, environmentScope)
	if err != nil {
		t.Fatal(err)
	}
	secretMetadataFound := false
	variableValueFound := false
	for _, entry := range entries {
		if entry.Name == "DEPLOY_TOKEN" && entry.Secret && entry.KeyID == "actions-test-key" && entry.Value == "" {
			secretMetadataFound = true
		}
		if entry.Name == "SHARED" && !entry.Secret && entry.Value == "environment" {
			variableValueFound = true
		}
	}
	if len(entries) != 2 || !secretMetadataFound || !variableValueFound {
		t.Fatalf("unexpected environment metadata: %#v", entries)
	}

	request := ExecutionContextRequest{
		Principal:      CredentialPrincipal{Kind: "service", Subject: "lorehub-ci-runner"},
		RepositoryID:   fixture.repositoryID,
		OrganizationID: fixture.organizationID,
		JobID:          job.ID,
		Environment:    "production",
		RequestedScope: "actions:execute",
	}
	resolved, err := resolver.Resolve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.OrganizationVariables["SHARED"] != "organization" ||
		resolved.RepositoryVariables["SHARED"] != "repository" ||
		resolved.EnvironmentVariables["SHARED"] != "environment" {
		t.Fatalf("scoped variables were not resolved independently: %#v", resolved)
	}
	if resolved.OrganizationSecrets["ORG_TOKEN"] != "organization-secret" ||
		resolved.RepositorySecrets["REPO_TOKEN"] != "repository-secret" ||
		resolved.EnvironmentSecrets["DEPLOY_TOKEN"] != "environment-secret-updated" {
		t.Fatalf("scoped secrets were not decrypted: %#v", resolved)
	}
	request.Principal.Subject = testCIRunnerPrincipalID
	if _, err := resolver.Resolve(ctx, request); err != nil {
		t.Fatalf("ci_runner UUID subject was rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE service_principal_repository_grants
		SET active = false WHERE principal_id = $1 AND repository_id = $2
	`, testCIRunnerPrincipalID, fixture.repositoryID); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(ctx, request); !errors.Is(err, ErrExecutionContextUnauthorized) {
		t.Fatalf("inactive ci_runner grant was accepted: %v", err)
	}
	grantExecutionContextRunner(t, pool, fixture.repositoryID, fixture.userID)
	if _, err := pool.Exec(ctx, `
		UPDATE service_principals SET active = false WHERE id = $1
	`, testCIRunnerPrincipalID); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(ctx, request); !errors.Is(err, ErrExecutionContextUnauthorized) {
		t.Fatalf("inactive ci_runner principal was accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE service_principals SET active = true WHERE id = $1
	`, testCIRunnerPrincipalID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE service_principal_repository_grants
		SET permissions = ARRAY['write']::varchar[]
		WHERE principal_id = $1 AND repository_id = $2
	`, testCIRunnerPrincipalID, fixture.repositoryID); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(ctx, request); !errors.Is(err, ErrExecutionContextUnauthorized) {
		t.Fatalf("ci_runner grant without read permission was accepted: %v", err)
	}
	grantExecutionContextRunner(t, pool, fixture.repositoryID, fixture.userID)

	request.Principal.Kind = "user"
	if _, err := resolver.Resolve(ctx, request); !errors.Is(err, ErrExecutionContextUnauthorized) {
		t.Fatalf("non-service execution principal was accepted: %v", err)
	}
	request.Principal.Kind = "service"
	request.OrganizationID = uuid.NewString()
	if _, err := resolver.Resolve(ctx, request); !errors.Is(err, ErrExecutionContextUnauthorized) {
		t.Fatalf("cross-organization execution request was accepted: %v", err)
	}

	if err := resolver.DeleteExecutionContextEntry(
		ctx, fixture.userID, repositoryScope, "SHARED", false,
	); err != nil {
		t.Fatal(err)
	}
	if err := resolver.DeleteExecutionContextEntry(
		ctx, fixture.userID, repositoryScope, "SHARED", false,
	); !errors.Is(err, ErrExecutionContextEntryNotFound) {
		t.Fatalf("missing execution context entry was not reported: %v", err)
	}
	assertExecutionContextAuditSafety(t, pool, fixture.userID)
}

func upsertExecutionContextFixture(
	t *testing.T,
	resolver *PostgresExecutionContextResolver,
	actorID string,
	organizationScope ExecutionContextScope,
	repositoryScope ExecutionContextScope,
	environmentScope ExecutionContextScope,
) {
	t.Helper()
	ctx := context.Background()
	for _, item := range []struct {
		scope ExecutionContextScope
		name  string
		value string
	}{
		{scope: organizationScope, name: "SHARED", value: "organization"},
		{scope: repositoryScope, name: "SHARED", value: "repository"},
		{scope: environmentScope, name: "SHARED", value: "environment"},
	} {
		if _, err := resolver.UpsertVariable(ctx, item.scope, item.name, item.value, actorID); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		scope ExecutionContextScope
		name  string
		value string
	}{
		{scope: organizationScope, name: "ORG_TOKEN", value: "organization-secret"},
		{scope: repositoryScope, name: "REPO_TOKEN", value: "repository-secret"},
		{scope: environmentScope, name: "DEPLOY_TOKEN", value: "environment-secret"},
	} {
		if _, err := resolver.UpsertSecret(ctx, item.scope, item.name, item.value, actorID); err != nil {
			t.Fatal(err)
		}
	}
}

func assertExecutionContextAuditSafety(t *testing.T, pool *pgxpool.Pool, actorID string) {
	t.Helper()
	var count, leaked int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*), COUNT(*) FILTER (
			WHERE details::text LIKE '%organization-secret%'
			   OR details::text LIKE '%repository-secret%'
			   OR details::text LIKE '%environment-secret%'
		)
		FROM audit_events
		WHERE actor_id = $1 AND target_type = 'actions_execution_context'
	`, actorID).Scan(&count, &leaked); err != nil {
		t.Fatal(err)
	}
	if count < 9 || leaked != 0 {
		t.Fatalf("execution context audit was incomplete or leaked a secret: count=%d leaked=%d", count, leaked)
	}
}

func TestExecutionContextManagementAuthorizationIntegration(t *testing.T) {
	ctx := context.Background()
	pool := openExecutionContextTestDB(t)
	defer pool.Close()
	fixture := newActionsFixture(t, pool)
	defer fixture.cleanup(t)
	resolver := newTestExecutionContextResolver(t, pool)
	actors := seedExecutionContextManagementActors(t, pool, fixture)
	defer cleanupExecutionContextManagementActors(t, pool, fixture.userID, actors)

	organizationScope := ExecutionContextScope{
		Kind: executionContextScopeOrganization, OrganizationID: fixture.organizationID,
	}
	repositoryScope := ExecutionContextScope{
		Kind: executionContextScopeRepository, OrganizationID: fixture.organizationID,
		RepositoryID: fixture.repositoryID,
	}
	for _, actorID := range []string{fixture.userID, actors.maintainer} {
		if _, err := resolver.ListExecutionContextEntries(ctx, actorID, organizationScope); err != nil {
			t.Fatalf("authorized organization manager %s was rejected: %v", actorID, err)
		}
	}
	for _, actorID := range []string{actors.member, actors.directAdmin, actors.suspendedAdmin} {
		_, err := resolver.ListExecutionContextEntries(ctx, actorID, organizationScope)
		if !errors.Is(err, ErrExecutionContextUnauthorized) {
			t.Fatalf("unauthorized organization manager %s was accepted: %v", actorID, err)
		}
	}
	for _, actorID := range []string{fixture.userID, actors.directAdmin, actors.teamAdmin} {
		if _, err := resolver.ListExecutionContextEntries(ctx, actorID, repositoryScope); err != nil {
			t.Fatalf("authorized repository admin %s was rejected: %v", actorID, err)
		}
	}
	for _, actorID := range []string{actors.maintainer, actors.member, actors.suspendedAdmin} {
		_, err := resolver.ListExecutionContextEntries(ctx, actorID, repositoryScope)
		if !errors.Is(err, ErrExecutionContextUnauthorized) {
			t.Fatalf("unauthorized repository manager %s was accepted: %v", actorID, err)
		}
	}
	if _, err := pool.Exec(ctx, `
		UPDATE users SET status = 'inactive' WHERE id = $1
	`, actors.suspendedAdmin); err != nil {
		t.Fatal(err)
	}
	_, err := resolver.ListExecutionContextEntries(ctx, actors.suspendedAdmin, repositoryScope)
	if !errors.Is(err, ErrExecutionContextUnauthorized) {
		t.Fatalf("inactive repository admin was accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE repository_memberships SET active = false
		WHERE repository_id = $1 AND user_id = $2
	`, fixture.repositoryID, actors.directAdmin); err != nil {
		t.Fatal(err)
	}
	_, err = resolver.ListExecutionContextEntries(ctx, actors.directAdmin, repositoryScope)
	if !errors.Is(err, ErrExecutionContextUnauthorized) {
		t.Fatalf("inactive direct repository admin was accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE repository_memberships SET active = true
		WHERE repository_id = $1 AND user_id = $2
	`, fixture.repositoryID, actors.directAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE team_repository_roles SET active = false
		WHERE repository_id = $1 AND team_id = $2
	`, fixture.repositoryID, actors.teamID); err != nil {
		t.Fatal(err)
	}
	_, err = resolver.ListExecutionContextEntries(ctx, actors.teamAdmin, repositoryScope)
	if !errors.Is(err, ErrExecutionContextUnauthorized) {
		t.Fatalf("inactive team repository admin was accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE team_repository_roles SET active = true
		WHERE repository_id = $1 AND team_id = $2
	`, fixture.repositoryID, actors.teamID); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE repositories SET lifecycle_state = 'failed' WHERE id = $1
	`, fixture.repositoryID); err != nil {
		t.Fatal(err)
	}
	_, err = resolver.ListExecutionContextEntries(ctx, fixture.userID, repositoryScope)
	if !errors.Is(err, ErrExecutionContextUnauthorized) {
		t.Fatalf("inactive repository was manageable: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE repositories SET lifecycle_state = 'active' WHERE id = $1
	`, fixture.repositoryID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE repositories SET archived_at = now(), archived_by = $2 WHERE id = $1
	`, fixture.repositoryID, fixture.userID); err != nil {
		t.Fatal(err)
	}
	_, err = resolver.ListExecutionContextEntries(ctx, fixture.userID, repositoryScope)
	if !errors.Is(err, ErrExecutionContextUnauthorized) {
		t.Fatalf("archived repository was manageable: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE repositories SET archived_at = NULL, archived_by = NULL WHERE id = $1
	`, fixture.repositoryID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE organizations SET active = false WHERE id = $1
	`, fixture.organizationID); err != nil {
		t.Fatal(err)
	}
	_, err = resolver.ListExecutionContextEntries(ctx, fixture.userID, organizationScope)
	if !errors.Is(err, ErrExecutionContextUnauthorized) {
		t.Fatalf("inactive organization was manageable: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE organizations SET active = true WHERE id = $1
	`, fixture.organizationID); err != nil {
		t.Fatal(err)
	}
}

func TestExecutionContextDatabaseConstraintsIntegration(t *testing.T) {
	ctx := context.Background()
	pool := openExecutionContextTestDB(t)
	defer pool.Close()
	fixture := newActionsFixture(t, pool)
	defer fixture.cleanup(t)
	resolver := newTestExecutionContextResolver(t, pool)
	defer deleteExecutionContextAudits(t, pool, fixture.userID)
	repositoryScope := ExecutionContextScope{
		Kind: executionContextScopeRepository, OrganizationID: fixture.organizationID,
		RepositoryID: fixture.repositoryID,
	}
	if _, err := resolver.UpsertVariable(ctx, repositoryScope, "Channel", "stable", fixture.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.UpsertVariable(ctx, repositoryScope, "channel", "beta", fixture.userID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM actions_execution_context_entries
		WHERE repository_id = $1 AND value_kind = 'variable' AND lower(name) = 'channel'
	`, fixture.repositoryID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("case-insensitive partial uniqueness was not enforced: %d", count)
	}

	invalidScopeID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO actions_execution_context_entries (
			id, organization_id, repository_id, scope_kind, value_kind,
			name, variable_value, updated_by
		) VALUES ($1, $2, $3, 'organization', 'variable', 'VALID_NAME', 'value', $4)
	`, invalidScopeID, fixture.organizationID, fixture.repositoryID, fixture.userID); err == nil {
		t.Fatal("inconsistent organization scope was accepted by PostgreSQL")
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO actions_execution_context_entries (
			id, organization_id, repository_id, scope_kind, value_kind,
			name, variable_value, updated_by
		) VALUES ($1, $2, $3, 'repository', 'variable', 'GITHUB_TOKEN', 'value', $4)
	`, uuid.NewString(), fixture.organizationID, fixture.repositoryID, fixture.userID); err == nil {
		t.Fatal("reserved execution context name was accepted by PostgreSQL")
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO actions_execution_context_entries (
			id, organization_id, repository_id, scope_kind, value_kind,
			name, variable_value, updated_by
		) VALUES ($1, $2, $3, 'repository', 'variable', 'lowercase', 'value', $4)
	`, uuid.NewString(), fixture.organizationID, fixture.repositoryID, fixture.userID); err == nil {
		t.Fatal("non-canonical execution context name was accepted by PostgreSQL")
	}

	otherOrganizationID, otherRepositoryID := seedOtherExecutionContextRepository(t, pool, fixture.userID)
	defer func() {
		if _, err := pool.Exec(ctx, "DELETE FROM organizations WHERE id = $1", otherOrganizationID); err != nil {
			t.Error(err)
		}
	}()
	if _, err := pool.Exec(ctx, `
		INSERT INTO actions_execution_context_entries (
			id, organization_id, repository_id, scope_kind, value_kind,
			name, variable_value, updated_by
		) VALUES ($1, $2, $3, 'repository', 'variable', 'BOUNDARY', 'value', $4)
	`, uuid.NewString(), fixture.organizationID, otherRepositoryID, fixture.userID); err == nil {
		t.Fatal("cross-organization repository scope was accepted by PostgreSQL")
	}
}

type executionContextManagementActors struct {
	maintainer     string
	member         string
	directAdmin    string
	teamAdmin      string
	suspendedAdmin string
	teamID         string
}

func seedExecutionContextManagementActors(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture actionsFixture,
) executionContextManagementActors {
	t.Helper()
	actors := executionContextManagementActors{
		maintainer: uuid.NewString(), member: uuid.NewString(), directAdmin: uuid.NewString(),
		teamAdmin: uuid.NewString(), suspendedAdmin: uuid.NewString(), teamID: uuid.NewString(),
	}
	ctx := context.Background()
	for label, userID := range map[string]string{
		"maintainer": actors.maintainer, "member": actors.member, "direct": actors.directAdmin,
		"team": actors.teamAdmin, "suspended": actors.suspendedAdmin,
	} {
		userStatus := "active"
		if label == "suspended" {
			userStatus = "suspended"
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO users (id, username, display_name, status) VALUES ($1, $2, $2, $3)
		`, userID, "context-"+label+"-"+strings.ToLower(uuid.NewString()[:8]), userStatus)
		if err != nil {
			t.Fatal(err)
		}
	}
	for userID, role := range map[string]string{
		actors.maintainer: "maintainer", actors.member: "member", actors.teamAdmin: "member",
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO organization_memberships (organization_id, user_id, role)
			VALUES ($1, $2, $3)
		`, fixture.organizationID, userID, role); err != nil {
			t.Fatal(err)
		}
	}
	for _, userID := range []string{actors.directAdmin, actors.suspendedAdmin} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO repository_memberships (repository_id, user_id, role)
			VALUES ($1, $2, 'admin')
		`, fixture.repositoryID, userID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO teams (id, organization_id, slug, display_name, created_by)
		VALUES ($1, $2, $3, 'Context Admins', $4)
	`, actors.teamID, fixture.organizationID, "context-admins-"+strings.ToLower(uuid.NewString()[:8]),
		fixture.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO team_memberships (team_id, user_id, role) VALUES ($1, $2, 'member')
	`, actors.teamID, actors.teamAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO team_repository_roles (team_id, repository_id, role, created_by)
		VALUES ($1, $2, 'admin', $3)
	`, actors.teamID, fixture.repositoryID, fixture.userID); err != nil {
		t.Fatal(err)
	}
	return actors
}

func cleanupExecutionContextManagementActors(
	t *testing.T,
	pool *pgxpool.Pool,
	ownerID string,
	actors executionContextManagementActors,
) {
	t.Helper()
	ctx := context.Background()
	actorIDs := []string{
		ownerID, actors.maintainer, actors.member, actors.directAdmin, actors.teamAdmin, actors.suspendedAdmin,
	}
	if _, err := pool.Exec(ctx, `
		DELETE FROM audit_events
		WHERE actor_id = ANY($1::uuid[]) AND target_type = 'actions_execution_context'
	`, actorIDs); err != nil {
		t.Error(err)
	}
	if _, err := pool.Exec(ctx, `
		DELETE FROM users WHERE id = ANY($1::uuid[])
	`, actorIDs[1:]); err != nil {
		t.Error(err)
	}
}

func openExecutionContextTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("LOREHUB_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("LOREHUB_TEST_DATABASE_URL or DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := database.Open(ctx, databaseURL, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	return pool
}

func grantExecutionContextRunner(
	t *testing.T,
	pool *pgxpool.Pool,
	repositoryID string,
	actorID string,
) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO service_principal_repository_grants (
			principal_id, repository_id, permissions, active, created_by
		) VALUES ($1, $2, ARRAY['read']::varchar[], true, $3)
		ON CONFLICT (principal_id, repository_id) DO UPDATE SET
			permissions = ARRAY['read']::varchar[], active = true, updated_at = now()
	`, testCIRunnerPrincipalID, repositoryID, actorID)
	if err != nil {
		t.Fatal(err)
	}
}

func deleteExecutionContextAudits(t *testing.T, pool *pgxpool.Pool, actorID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		DELETE FROM audit_events
		WHERE actor_id = $1 AND target_type = 'actions_execution_context'
	`, actorID)
	if err != nil {
		t.Error(err)
	}
}

func seedOtherExecutionContextRepository(
	t *testing.T,
	pool *pgxpool.Pool,
	actorID string,
) (string, string) {
	t.Helper()
	organizationID := uuid.NewString()
	repositoryID := uuid.NewString()
	suffix := strings.ToLower(uuid.NewString()[:8])
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO organizations (id, slug, display_name, created_by)
		VALUES ($1, $2, 'Other Context Organization', $3)
	`, organizationID, "context-other-"+suffix, actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, lore_repository_id,
			lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Other Context Repository', $4, $5, 'main', $6)
	`, repositoryID, organizationID, "repository-"+suffix,
		strings.ReplaceAll(repositoryID, "-", ""), "lore://fixture/"+repositoryID, actorID); err != nil {
		t.Fatal(err)
	}
	return organizationID, repositoryID
}
