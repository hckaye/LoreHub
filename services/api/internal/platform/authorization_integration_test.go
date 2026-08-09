package platform

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/authz"
	"github.com/lorehub/lorehub/services/api/internal/database"
)

type authorizationFixture struct {
	pool        *pgxpool.Pool
	store       *Store
	orgID       string
	orgSlug     string
	repositoryA string
	repositoryB string
	loreA       string
	loreB       string
	manager     User
	alice       User
	bob         User
}

func authorizationIntegrationFixture(t *testing.T) authorizationFixture {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set; skipping PostgreSQL authorization tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, os.Getenv("DATABASE_URL"), 5*time.Second)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("migrate database: %v", err)
	}
	t.Cleanup(pool.Close)
	store := NewStore(pool)
	seed := uuid.NewString()
	fixture := authorizationFixture{
		pool:        pool,
		store:       store,
		orgID:       uuid.NewString(),
		orgSlug:     "authz-" + strings.ReplaceAll(seed[:8], "-", ""),
		repositoryA: uuid.NewString(),
		repositoryB: uuid.NewString(),
		loreA:       strings.ReplaceAll(uuid.NewString(), "-", ""),
		loreB:       strings.ReplaceAll(uuid.NewString(), "-", ""),
		manager:     User{ID: uuid.NewString(), Username: "manager-" + seed[:8], DisplayName: "管理者"},
		alice:       User{ID: uuid.NewString(), Username: "alice-" + seed[:8], DisplayName: "アリス"},
		bob:         User{ID: uuid.NewString(), Username: "bob-" + seed[:8], DisplayName: "ボブ"},
	}
	ctx = context.Background()
	authorizationMustExec(t, pool, `
		INSERT INTO users (id, username, display_name) VALUES
		($1, $2, $3), ($4, $5, $6), ($7, $8, $9)
	`, fixture.manager.ID, fixture.manager.Username, fixture.manager.DisplayName,
		fixture.alice.ID, fixture.alice.Username, fixture.alice.DisplayName,
		fixture.bob.ID, fixture.bob.Username, fixture.bob.DisplayName)
	authorizationMustExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, description, visibility, created_by)
		VALUES ($1, $2, '認可検証', '', 'private', $3)
	`, fixture.orgID, fixture.orgSlug, fixture.manager.ID)
	authorizationMustExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role) VALUES
		($1, $2, 'owner'), ($1, $3, 'member'), ($1, $4, 'member')
	`, fixture.orgID, fixture.manager.ID, fixture.alice.ID, fixture.bob.ID)
	authorizationMustExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES
		($1, $2, 'a', 'A', '', 'private', $3, 'lore://a', 'main', $5),
		($6, $2, 'b', 'B', '', 'private', $4, 'lore://b', 'main', $5)
	`, fixture.repositoryA, fixture.orgID, fixture.loreA, fixture.loreB, fixture.manager.ID, fixture.repositoryB)
	authorizationMustExec(t, pool, `
		INSERT INTO repository_counters (repository_id) VALUES ($1), ($2)
	`, fixture.repositoryA, fixture.repositoryB)
	authorizationMustExec(t, pool, `
		INSERT INTO repository_policies (repository_id) VALUES ($1), ($2)
	`, fixture.repositoryA, fixture.repositoryB)
	authorizationMustExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role) VALUES ($1, $2, 'write')
	`, fixture.repositoryA, fixture.alice.ID)
	authorizationMustExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role) VALUES ($1, $2, 'write')
	`, fixture.repositoryB, fixture.bob.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, fixture.orgID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1, $2, $3)`,
			fixture.manager.ID, fixture.alice.ID, fixture.bob.ID)
	})
	return fixture
}

func authorizationMustExec(t *testing.T, pool *pgxpool.Pool, query string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), query, args...); err != nil {
		t.Fatalf("fixture SQL failed: %v", err)
	}
}

func TestAuthorizationIntegrationPartitionsTeamsAndRevocation(t *testing.T) {
	fixture := authorizationIntegrationFixture(t)
	ctx := context.Background()
	resourceA := "urc-" + fixture.loreA
	resourceB := "urc-" + fixture.loreB

	permissions, err := fixture.store.EffectivePermissions(ctx, fixture.alice.ID, resourceA)
	if err != nil {
		t.Fatalf("Alice A permissions: %v", err)
	}
	if !containsPermission(permissions.Permissions, authz.PermissionWrite) {
		t.Fatalf("Alice A permissions = %v, want write", permissions.Permissions)
	}
	permissions, err = fixture.store.EffectivePermissions(ctx, fixture.alice.ID, resourceB)
	if err != nil {
		t.Fatalf("Alice B permissions: %v", err)
	}
	if len(permissions.Permissions) != 0 {
		t.Fatalf("Alice B permissions = %v, want none", permissions.Permissions)
	}
	if _, err := fixture.store.RepositoryForRead(ctx, &fixture.alice, fixture.orgSlug, "b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Alice should receive private 404 for B, got %v", err)
	}
	resources, _, err := fixture.store.ListResourcePermissions(ctx, fixture.alice.ID, "urc", 50, "")
	if err != nil {
		t.Fatalf("list Alice resources: %v", err)
	}
	if len(resources) != 1 || resources[0].ResourceID != resourceA {
		t.Fatalf("Alice resources = %+v, want only A", resources)
	}
	organizationMembers, err := fixture.store.ListOrganizationMembers(ctx, fixture.manager, fixture.orgSlug)
	if err != nil || len(organizationMembers) != 3 {
		t.Fatalf("organization members = %+v, err %v", organizationMembers, err)
	}
	if _, err := fixture.store.SetOrganizationMember(ctx, fixture.manager, fixture.orgSlug,
		SetOrganizationMemberInput{
			Username: fixture.manager.Username,
			Role:     "member",
			Active:   true,
		}); !errors.Is(err, ErrConflict) {
		t.Fatalf("demoting the only organization owner error = %v", err)
	}
	if _, err := fixture.store.SetOrganizationMember(ctx, fixture.manager, fixture.orgSlug,
		SetOrganizationMemberInput{Username: fixture.bob.Username, Role: "member", Active: false}); err != nil {
		t.Fatalf("deactivate Bob organization membership: %v", err)
	}
	permissions, err = fixture.store.EffectivePermissions(ctx, fixture.bob.ID, resourceB)
	if err != nil || len(permissions.Permissions) != 0 {
		t.Fatalf("Bob B permissions after organization revoke = %v, err %v", permissions.Permissions, err)
	}
	if _, err := fixture.store.SetOrganizationMember(ctx, fixture.manager, fixture.orgSlug,
		SetOrganizationMemberInput{Username: fixture.bob.Username, Role: "member", Active: true}); err != nil {
		t.Fatalf("restore Bob organization membership: %v", err)
	}

	team, err := fixture.store.CreateTeam(ctx, fixture.manager, fixture.orgSlug, SetTeamInput{
		Slug: "lore-readers", DisplayName: "Lore 読み取りチーム",
	})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if _, err := fixture.store.SetTeamMember(ctx, fixture.manager, fixture.orgSlug, team.Slug,
		SetTeamMemberInput{Username: fixture.alice.Username, Role: "member", Active: true}); err != nil {
		t.Fatalf("add Alice to team: %v", err)
	}
	if _, err := fixture.store.SetTeamRepositoryRole(ctx, fixture.manager, fixture.orgSlug, team.Slug,
		fixture.orgSlug, "b", SetTeamRepositoryRoleInput{Role: "read"}); err != nil {
		t.Fatalf("grant team B read: %v", err)
	}
	permissions, err = fixture.store.EffectivePermissions(ctx, fixture.alice.ID, resourceB)
	if err != nil || !containsPermission(permissions.Permissions, authz.PermissionRead) {
		t.Fatalf("Alice team B permissions = %v, err %v", permissions.Permissions, err)
	}
	authorizationMustExec(t, fixture.pool, `UPDATE teams SET active = false WHERE id = $1`, team.ID)
	permissions, err = fixture.store.EffectivePermissions(ctx, fixture.alice.ID, resourceB)
	if err != nil || len(permissions.Permissions) != 0 {
		t.Fatalf("inactive team permissions = %v, err %v", permissions.Permissions, err)
	}
	authorizationMustExec(t, fixture.pool, `UPDATE teams SET active = true WHERE id = $1`, team.ID)
	if err := fixture.store.DeleteTeamRepositoryRole(ctx, fixture.manager, fixture.orgSlug, team.Slug,
		fixture.orgSlug, "b"); err != nil {
		t.Fatalf("delete team B role: %v", err)
	}
	permissions, err = fixture.store.EffectivePermissions(ctx, fixture.alice.ID, resourceB)
	if err != nil || len(permissions.Permissions) != 0 {
		t.Fatalf("Alice B permissions after team role delete = %v, err %v", permissions.Permissions, err)
	}
	if _, err := fixture.store.SetTeamRepositoryRole(ctx, fixture.manager, fixture.orgSlug, team.Slug,
		fixture.orgSlug, "b", SetTeamRepositoryRoleInput{Role: "read"}); err != nil {
		t.Fatalf("restore team B read: %v", err)
	}
	if _, err := fixture.store.SetTeamMember(ctx, fixture.manager, fixture.orgSlug, team.Slug,
		SetTeamMemberInput{Username: fixture.alice.Username, Role: "member", Active: false}); err != nil {
		t.Fatalf("revoke Alice team membership: %v", err)
	}
	permissions, err = fixture.store.EffectivePermissions(ctx, fixture.alice.ID, resourceB)
	if err != nil {
		t.Fatalf("Alice B permissions after revoke: %v", err)
	}
	if len(permissions.Permissions) != 0 {
		t.Fatalf("Alice B permissions after revoke = %v, want none", permissions.Permissions)
	}

	if _, err := fixture.store.SetRepositoryCollaborator(ctx, fixture.manager, fixture.orgSlug, "a",
		SetCollaboratorInput{Username: fixture.alice.Username, Role: "write", Active: false}); err != nil {
		t.Fatalf("revoke Alice direct role: %v", err)
	}
	permissions, err = fixture.store.EffectivePermissions(ctx, fixture.alice.ID, resourceA)
	if err != nil {
		t.Fatalf("Alice A permissions after revoke: %v", err)
	}
	if len(permissions.Permissions) != 0 {
		t.Fatalf("Alice A permissions after revoke = %v, want none", permissions.Permissions)
	}
	authorizationMustExec(t, fixture.pool, `UPDATE repositories SET visibility = 'public' WHERE id = $1`,
		fixture.repositoryA)
	permissions, err = fixture.store.EffectivePermissions(ctx, fixture.alice.ID, resourceA)
	if err != nil || !containsPermission(permissions.Permissions, authz.PermissionRead) {
		t.Fatalf("Alice public A permissions = %v, err %v", permissions.Permissions, err)
	}
	if _, err := fixture.store.RepositoryForRead(ctx, &fixture.alice, fixture.orgSlug, "a"); err != nil {
		t.Fatalf("active member should read public A: %v", err)
	}
	if _, err := fixture.store.SetOrganizationMember(ctx, fixture.manager, fixture.orgSlug,
		SetOrganizationMemberInput{Username: fixture.alice.Username, Role: "member", Active: false}); err != nil {
		t.Fatalf("deactivate Alice organization membership: %v", err)
	}
	if _, err := fixture.store.RepositoryForRead(ctx, &fixture.alice, fixture.orgSlug, "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inactive member should not fall back to public access: %v", err)
	}

	var auditCount int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_events
		WHERE organization_id = $1 AND action IN ('team.create', 'team.member.set',
		'team.repository_role.set', 'team.repository_role.delete', 'repository.collaborator.set',
		'organization.member.set')
	`, fixture.orgID).Scan(&auditCount); err != nil {
		t.Fatalf("count authorization audit events: %v", err)
	}
	if auditCount < 4 {
		t.Fatalf("authorization audit count = %d, want at least 4", auditCount)
	}
}

func TestAuthorizationIntegrationProtectedBranchAndOneTimeMerge(t *testing.T) {
	fixture := authorizationIntegrationFixture(t)
	ctx := context.Background()
	resourceA := "urc-" + fixture.loreA
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO branch_rules (id, repository_id, pattern, required_approvals, block_direct_push)
		VALUES ($1, $2, 'main', 0, true)
	`, uuid.New(), fixture.repositoryA)
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO repository_branch_states (repository_id, branch_id, branch_name, latest_revision)
		VALUES ($1, 'main-id', 'main', 'base-revision')
	`, fixture.repositoryA)
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO repository_branch_states (repository_id, branch_id, branch_name, latest_revision)
		VALUES ($1, 'feature-id', 'feature', 'base-revision')
	`, fixture.repositoryA)
	decision, err := fixture.store.CheckPolicy(ctx, authz.PolicyCheck{
		UserID: fixture.alice.ID, ResourceID: resourceA, Operation: authz.OperationBranchPush,
		BranchID:         "main-id",
		ProposedRevision: "direct-revision",
	})
	if err != nil {
		t.Fatalf("protected branch check: %v", err)
	}
	if decision.Allowed {
		t.Fatal("direct protected branch push should be denied")
	}
	decision, err = fixture.store.CheckPolicy(ctx, authz.PolicyCheck{
		UserID: fixture.alice.ID, ResourceID: resourceA, Operation: authz.OperationBranchCreate,
		BranchID: "new-branch-id",
	})
	if err != nil || !decision.Allowed {
		t.Fatalf("branch create should require only general write: %+v, err %v", decision, err)
	}
	decision, err = fixture.store.CheckPolicy(ctx, authz.PolicyCheck{
		UserID: fixture.alice.ID, ResourceID: resourceA, Operation: authz.OperationBranchPush,
		BranchID: "unobserved-branch-id", ProposedRevision: "new-revision",
	})
	if err != nil || decision.Allowed {
		t.Fatalf("unobserved branch push should fail closed: %+v, err %v", decision, err)
	}
	decision, err = fixture.store.CheckPolicy(ctx, authz.PolicyCheck{
		UserID: fixture.alice.ID, ResourceID: resourceA, Operation: authz.OperationBranchPush,
		BranchID:         "feature-id",
		ProposedRevision: "feature-revision",
	})
	if err != nil || !decision.Allowed {
		t.Fatalf("feature branch push decision = %+v, err %v", decision, err)
	}

	err = fixture.store.PrepareMergeAuthorization(ctx, fixture.alice.ID, MergeAuthorizationInput{
		RepositoryID: fixture.loreA, BranchID: "main-id", BranchName: "main",
		ExpectedBase: "base-revision", ExpectedHead: "merge-revision", SourceRevision: "source-revision",
		Lifetime: time.Minute,
	})
	if err != nil {
		t.Fatalf("issue merge authorization: %v", err)
	}
	decision, err = fixture.store.CheckPolicy(ctx, authz.PolicyCheck{
		UserID: fixture.alice.ID, ResourceID: resourceA, Operation: authz.OperationMerge,
		BranchID: "main-id", ProposedRevision: "wrong-revision",
	})
	if err != nil || decision.Allowed {
		t.Fatalf("wrong merge tuple should be denied: %+v, err %v", decision, err)
	}
	decision, err = fixture.store.CheckPolicy(ctx, authz.PolicyCheck{
		UserID: fixture.alice.ID, ResourceID: resourceA, Operation: authz.OperationMerge,
		BranchID: "main-id", ProposedRevision: "merge-revision",
	})
	if err != nil || !decision.Allowed {
		t.Fatalf("merge authorization decision = %+v, err %v", decision, err)
	}
	decision, err = fixture.store.CheckPolicy(ctx, authz.PolicyCheck{
		UserID: fixture.alice.ID, ResourceID: resourceA, Operation: authz.OperationMerge,
		BranchID: "main-id", ProposedRevision: "merge-revision",
	})
	if err != nil {
		t.Fatalf("replayed merge authorization: %v", err)
	}
	if decision.Allowed {
		t.Fatal("merge authorization should be single-use")
	}

	err = fixture.store.PrepareMergeAuthorization(ctx, fixture.alice.ID, MergeAuthorizationInput{
		RepositoryID: fixture.loreA, BranchID: "main-id", BranchName: "main",
		ExpectedBase: "base-revision", ExpectedHead: "merge-revision", SourceRevision: "source-revision",
		Lifetime: time.Minute,
	})
	if err != nil {
		t.Fatalf("issue concurrent merge authorization: %v", err)
	}
	results := make(chan bool, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			got, checkErr := fixture.store.CheckPolicy(ctx, authz.PolicyCheck{
				UserID: fixture.alice.ID, ResourceID: resourceA, Operation: authz.OperationBranchPush,
				BranchID:         "main-id",
				ProposedRevision: "merge-revision",
			})
			results <- checkErr == nil && got.Allowed
		}()
	}
	wait.Wait()
	close(results)
	allowed := 0
	for result := range results {
		if result {
			allowed++
		}
	}
	if allowed != 1 {
		t.Fatalf("concurrent pending merge decisions allowed = %d, want 1", allowed)
	}
	err = fixture.store.PrepareMergeAuthorization(ctx, fixture.alice.ID, MergeAuthorizationInput{
		RepositoryID: fixture.loreA, BranchID: "main-id", BranchName: "main",
		ExpectedBase: "base-revision", ExpectedHead: "expired-revision", SourceRevision: "source-revision",
		Lifetime: time.Minute,
	})
	if err != nil {
		t.Fatalf("issue expiring merge authorization: %v", err)
	}
	authorizationMustExec(t, fixture.pool, `
		UPDATE lore_merge_authorizations SET expires_at = created_at + interval '1 millisecond'
		WHERE repository_id = $1 AND proposed_revision = 'expired-revision'
	`, fixture.repositoryA)
	time.Sleep(10 * time.Millisecond)
	decision, err = fixture.store.CheckPolicy(ctx, authz.PolicyCheck{
		UserID: fixture.alice.ID, ResourceID: resourceA, Operation: authz.OperationMerge,
		BranchID: "main-id", ProposedRevision: "expired-revision",
	})
	if err != nil || decision.Allowed {
		t.Fatalf("expired merge tuple should be denied: %+v, err %v", decision, err)
	}

	authorizationMustExec(t, fixture.pool, `
		UPDATE branch_rules SET block_direct_push = false WHERE repository_id = $1
	`, fixture.repositoryA)
	decision, err = fixture.store.CheckPolicy(ctx, authz.PolicyCheck{
		UserID: fixture.alice.ID, ResourceID: resourceA, Operation: authz.OperationBranchPush,
		BranchID:         "main-id",
		ProposedRevision: "rule-change-revision",
	})
	if err != nil || !decision.Allowed {
		t.Fatalf("branch rule change decision = %+v, err %v", decision, err)
	}
}

func TestAuthorizationIntegrationProvisioningPrincipalsAndObservation(t *testing.T) {
	fixture := authorizationIntegrationFixture(t)
	ctx := context.Background()
	resourceA := "urc-" + fixture.loreA
	if err := fixture.store.SetServicePrincipalGrant(ctx, fixture.manager, "lorehub-anonymous-reader",
		fixture.orgSlug, "a", []string{authz.PermissionRead}, true); err != nil {
		t.Fatalf("grant anonymous reader on private repository: %v", err)
	}
	anonymous, _, err := fixture.store.ServicePrincipalResource(ctx, "lorehub-anonymous-reader", resourceA)
	if err != nil {
		t.Fatalf("read private anonymous principal: %v", err)
	}
	permissions, err := fixture.store.EffectivePermissions(ctx, anonymous.ID, resourceA)
	if err != nil || len(permissions.Permissions) != 0 {
		t.Fatalf("private anonymous permissions = %v, err %v", permissions.Permissions, err)
	}
	if err := fixture.store.SetServicePrincipalGrant(ctx, fixture.manager, "lorehub-observer",
		fixture.orgSlug, "a", []string{authz.PermissionObliterate}, true); err != nil {
		t.Fatalf("grant observer obliterate: %v", err)
	}
	observer, _, err := fixture.store.ServicePrincipalResource(ctx, "lorehub-observer", resourceA)
	if err != nil {
		t.Fatalf("read observer principal: %v", err)
	}
	permissions, err = fixture.store.EffectivePermissions(ctx, observer.ID, resourceA)
	if err != nil || containsPermission(permissions.Permissions, authz.PermissionObliterate) {
		t.Fatalf("disabled obliterate permissions = %v, err %v", permissions.Permissions, err)
	}
	if err := fixture.store.SetRepositoryPolicy(ctx, fixture.manager, fixture.orgSlug, "a",
		SetRepositoryPolicyInput{ObliterateEnabled: true}); err != nil {
		t.Fatalf("enable obliterate policy: %v", err)
	}
	permissions, err = fixture.store.EffectivePermissions(ctx, observer.ID, resourceA)
	if err != nil || !containsPermission(permissions.Permissions, authz.PermissionObliterate) {
		t.Fatalf("enabled obliterate permissions = %v, err %v", permissions.Permissions, err)
	}
	repository, err := fixture.store.BeginRepositoryProvisioning(ctx, fixture.manager, fixture.orgSlug,
		ProvisionRepositoryInput{
			Slug: "managed", DisplayName: "管理対象", Visibility: "public", DefaultBranch: "main",
		}, "lores://lore.lorehub.localhost:41337")
	if err != nil {
		t.Fatalf("begin provisioning: %v", err)
	}
	if len(repository.LoreRepositoryID) != 32 || repository.LifecycleState != "pending" {
		t.Fatalf("unexpected pending repository: %+v", repository)
	}
	permissions, err = fixture.store.EffectivePermissions(ctx, fixture.manager.ID,
		"urc-"+repository.LoreRepositoryID)
	if err != nil || !containsPermission(permissions.Permissions, authz.PermissionAdmin) {
		t.Fatalf("pending creator permissions = %v, err %v", permissions.Permissions, err)
	}
	provisioner, granted, err := fixture.store.ServicePrincipalResource(ctx, "lorehub-provisioner",
		"urc-"+repository.LoreRepositoryID)
	if err != nil || provisioner.ProviderSubject != "service:lorehub-provisioner" ||
		!containsPermission(granted, authz.PermissionAdmin) {
		t.Fatalf("pending provisioner grant = %+v/%v, err %v", provisioner, granted, err)
	}
	if err := fixture.store.MarkRepositoryProvisioningFailed(
		ctx, fixture.manager, repository.ID, "Lore unavailable"); err != nil {
		t.Fatalf("mark provisioning failed: %v", err)
	}
	retried, err := fixture.store.BeginRepositoryProvisioning(ctx, fixture.manager, fixture.orgSlug,
		ProvisionRepositoryInput{
			Slug: "managed", DisplayName: "管理対象", Visibility: "public", DefaultBranch: "main",
		}, "lores://lore.lorehub.localhost:41337")
	if err != nil || retried.ID != repository.ID || retried.LoreRepositoryID != repository.LoreRepositoryID {
		t.Fatalf("provision retry = %+v, err %v", retried, err)
	}
	if err := fixture.store.MarkRepositoryProvisioned(ctx, fixture.manager, repository.ID); err != nil {
		t.Fatalf("mark provisioning complete: %v", err)
	}
	if _, _, err := fixture.store.ServicePrincipalResource(ctx, "lorehub-provisioner",
		"urc-"+repository.LoreRepositoryID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("completed provisioning principal error = %v, want forbidden", err)
	}
	if err := fixture.store.SetServicePrincipalGrant(ctx, fixture.manager, "lorehub-ci-runner",
		fixture.orgSlug, "a", []string{authz.PermissionRead}, true); err != nil {
		t.Fatalf("grant CI principal: %v", err)
	}
	principal, granted, err := fixture.store.ServicePrincipalResource(ctx, "lorehub-ci-runner",
		"urc-"+fixture.loreA)
	if err != nil || !containsPermission(granted, authz.PermissionRead) {
		t.Fatalf("CI principal grant = %v, err %v", granted, err)
	}
	permissions, err = fixture.store.EffectivePermissions(ctx, principal.ID, "urc-"+fixture.loreA)
	if err != nil || !containsPermission(permissions.Permissions, authz.PermissionRead) {
		t.Fatalf("CI principal permissions = %v, err %v", permissions.Permissions, err)
	}
	if err := fixture.store.SetServicePrincipalGrant(ctx, fixture.manager, "lorehub-ci-runner",
		fixture.orgSlug, "a", []string{authz.PermissionRead}, false); err != nil {
		t.Fatalf("revoke CI principal: %v", err)
	}
	permissions, err = fixture.store.EffectivePermissions(ctx, principal.ID, "urc-"+fixture.loreA)
	if err != nil || len(permissions.Permissions) != 0 {
		t.Fatalf("revoked CI principal permissions = %v, err %v", permissions.Permissions, err)
	}

	authorizationMustExec(t, fixture.pool, `
		INSERT INTO repository_branch_states (repository_id, branch_id, branch_name, latest_revision)
		VALUES ($1, 'main-id', 'main', 'before-observation')
	`, fixture.repositoryA)
	if err := fixture.store.ObserveLoreBranchRevision(ctx, fixture.loreA, "main-id", "observed-revision"); err != nil {
		t.Fatalf("observe push revision: %v", err)
	}
	var currentRevision string
	if err := fixture.pool.QueryRow(ctx, `
		SELECT latest_revision FROM repository_branch_states
		WHERE repository_id = $1 AND branch_id = 'main-id'
	`, fixture.repositoryA).Scan(&currentRevision); err != nil || currentRevision != "observed-revision" {
		t.Fatalf("observed branch revision = %q, err %v", currentRevision, err)
	}
	if err := fixture.store.DeleteLoreBranchState(ctx, fixture.loreA, "main-id"); err != nil {
		t.Fatalf("observe branch deletion: %v", err)
	}
	var branchCount int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM repository_branch_states
		WHERE repository_id = $1 AND branch_id = 'main-id'
	`, fixture.repositoryA).Scan(&branchCount); err != nil || branchCount != 0 {
		t.Fatalf("deleted branch state count = %d, err %v", branchCount, err)
	}

	var auditCount, outboxCount int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_events
		WHERE organization_id = $1 AND action IN (
			'repository.provisioning.started', 'repository.provisioning.failed',
			'repository.provisioning.completed', 'service_principal.grant.set'
		)
	`, fixture.orgID).Scan(&auditCount); err != nil {
		t.Fatalf("count provisioning audits: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM outbox_events
		WHERE topic IN ('control.repository.provisioning.started',
			'control.repository.provisioning.failed', 'control.repository.provisioning.completed',
			'control.service_principal.grant.set')
	`).Scan(&outboxCount); err != nil {
		t.Fatalf("count provisioning outbox events: %v", err)
	}
	if auditCount < 4 || outboxCount < 4 {
		t.Fatalf("provisioning audit/outbox counts = %d/%d, want at least four", auditCount, outboxCount)
	}
}

func containsPermission(permissions []string, expected string) bool {
	for _, permission := range permissions {
		if permission == expected {
			return true
		}
	}
	return false
}
