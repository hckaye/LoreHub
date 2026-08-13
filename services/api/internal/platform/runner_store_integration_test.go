package platform

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/auth"
)

type runnerStoreFixture struct {
	store           *Store
	owner           User
	maintainer      User
	member          User
	repositoryAdmin User
	organizationID  string
	otherOrgID      string
	repositoryID    string
}

func TestRunnerLifecycleIntegration(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	fixture := newRunnerStoreFixture(t, store)
	ctx := context.Background()
	codec, err := auth.NewSecretCodec("runner lifecycle integration secret")
	if err != nil {
		t.Fatal(err)
	}
	registrationRaw, registrationDigest, err := auth.NewRunnerRegistrationToken(codec)
	if err != nil {
		t.Fatal(err)
	}
	registration, err := store.CreateRegistrationToken(ctx, fixture.owner,
		CreateRunnerRegistrationTokenInput{
			Scope: RunnerScope{
				OrganizationID: fixture.organizationID,
				RepositoryID:   fixture.repositoryID,
			},
			Digest:    registrationDigest,
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		})
	if err != nil {
		t.Fatal(err)
	}
	consumed, err := store.ConsumeRegistrationToken(ctx, codec.Digest(registrationRaw))
	if err != nil {
		t.Fatal(err)
	}
	if consumed.ID != registration.ID || consumed.ConsumedAt == nil ||
		consumed.Scope.RepositoryID != fixture.repositoryID ||
		consumed.Scope.OrganizationID != fixture.organizationID {
		t.Fatalf("unexpected consumed registration token: %+v", consumed)
	}
	if _, err := store.ConsumeRegistrationToken(
		ctx, codec.Digest(registrationRaw),
	); !errors.Is(err, auth.ErrInvalidRunnerToken) {
		t.Fatalf("consumed registration token was reusable: %v", err)
	}
	credentialRaw, credentialDigest, err := auth.NewRunnerCredential(codec)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := store.RegisterRunner(ctx, RegisterRunnerInput{
		RegistrationTokenID: consumed.ID,
		Name:                "Linux Builder",
		Labels:              []string{" X64 ", "LINUX", "linux", "self-hosted"},
		CredentialDigest:    credentialDigest,
		CredentialKeyID:     "runner-v1",
		CredentialExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour),
		RunnerVersion:       "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantLabels := []string{"linux", "self-hosted", "x64"}
	if runner.Name != "Linux Builder" || !equalStrings(runner.Labels, wantLabels) ||
		runner.Scope.RepositoryID != fixture.repositoryID {
		t.Fatalf("unexpected registered runner: %+v", runner)
	}
	usedAt := time.Now().UTC()
	authenticated, err := store.AuthenticateRunner(
		ctx, codec.Digest(credentialRaw), "runner-v1", usedAt,
	)
	if err != nil || authenticated.ID != runner.ID || authenticated.LastUsedAt == nil {
		t.Fatalf("authenticate runner: runner=%+v error=%v", authenticated, err)
	}
	if _, err := store.AuthenticateRunner(
		ctx, codec.Digest(credentialRaw), "other-key", usedAt,
	); !errors.Is(err, auth.ErrInvalidRunnerToken) {
		t.Fatalf("runner credential accepted under another key ID: %v", err)
	}
	seenAt := usedAt.Add(time.Second)
	if err := store.TouchRunnerSeen(ctx, runner.ID, seenAt); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListRunners(ctx, fixture.repositoryAdmin, runner.Scope)
	if err != nil || len(listed) != 1 || listed[0].ID != runner.ID || listed[0].LastSeenAt == nil {
		t.Fatalf("list runners: runners=%+v error=%v", listed, err)
	}
	if err := store.RevokeRunner(ctx, fixture.owner, runner.Scope, runner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateRunner(
		ctx, codec.Digest(credentialRaw), "runner-v1", time.Now().UTC(),
	); !errors.Is(err, auth.ErrInvalidRunnerToken) {
		t.Fatalf("revoked runner credential error = %v", err)
	}
	if err := store.TouchRunnerSeen(ctx, runner.ID, time.Now().UTC()); !errors.Is(
		err, auth.ErrInvalidRunnerToken,
	) {
		t.Fatalf("revoked runner heartbeat error = %v", err)
	}
	var auditCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_events
		WHERE target_id IN ($1, $2)
		  AND action IN ('runner.registration_token.create', 'runner.register', 'runner.revoke')
	`, registration.ID, runner.ID).Scan(&auditCount); err != nil || auditCount != 3 {
		t.Fatalf("runner audit count = %d, error=%v", auditCount, err)
	}
}

func TestRunnerRegistrationTokenAtomicConsumeIntegration(t *testing.T) {
	_, store := identityIntegrationStore(t)
	fixture := newRunnerStoreFixture(t, store)
	ctx := context.Background()
	codec, _ := auth.NewSecretCodec("runner atomic consume integration secret")
	raw, digest, err := auth.NewRunnerRegistrationToken(codec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateRegistrationToken(ctx, fixture.maintainer,
		CreateRunnerRegistrationTokenInput{
			Scope:     RunnerScope{OrganizationID: fixture.organizationID},
			Digest:    digest,
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		}); err != nil {
		t.Fatal(err)
	}

	const attempts = 8
	var successes atomic.Int32
	var unexpected atomic.Value
	var wait sync.WaitGroup
	start := make(chan struct{})
	for range attempts {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := store.ConsumeRegistrationToken(ctx, codec.Digest(raw))
			if err == nil {
				successes.Add(1)
				return
			}
			if !errors.Is(err, auth.ErrInvalidRunnerToken) {
				unexpected.Store(err)
			}
		}()
	}
	close(start)
	wait.Wait()
	if value := unexpected.Load(); value != nil {
		t.Fatalf("unexpected atomic consume error: %v", value)
	}
	if successes.Load() != 1 {
		t.Fatalf("atomic consume successes = %d, want 1", successes.Load())
	}
}

func TestRunnerScopeAuthorizationIntegration(t *testing.T) {
	_, store := identityIntegrationStore(t)
	fixture := newRunnerStoreFixture(t, store)
	ctx := context.Background()
	codec, _ := auth.NewSecretCodec("runner scope integration secret")
	newInput := func(scope RunnerScope) CreateRunnerRegistrationTokenInput {
		_, digest, err := auth.NewRunnerRegistrationToken(codec)
		if err != nil {
			t.Fatal(err)
		}
		return CreateRunnerRegistrationTokenInput{
			Scope: scope, Digest: digest, ExpiresAt: time.Now().UTC().Add(time.Hour),
		}
	}
	organizationScope := RunnerScope{OrganizationID: fixture.organizationID}
	if _, err := store.CreateRegistrationToken(
		ctx, fixture.maintainer, newInput(organizationScope),
	); err != nil {
		t.Fatalf("organization maintainer registration token: %v", err)
	}
	if _, err := store.CreateRegistrationToken(
		ctx, fixture.member, newInput(organizationScope),
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("organization member registration token error = %v", err)
	}
	repositoryScope := RunnerScope{
		OrganizationID: fixture.organizationID,
		RepositoryID:   fixture.repositoryID,
	}
	if _, err := store.CreateRegistrationToken(
		ctx, fixture.repositoryAdmin, newInput(repositoryScope),
	); err != nil {
		t.Fatalf("repository admin registration token: %v", err)
	}
	if _, err := store.CreateRegistrationToken(
		ctx, fixture.maintainer, newInput(repositoryScope),
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("organization maintainer repository token error = %v", err)
	}
	wrongBoundary := RunnerScope{
		OrganizationID: fixture.otherOrgID,
		RepositoryID:   fixture.repositoryID,
	}
	if _, err := store.CreateRegistrationToken(
		ctx, fixture.owner, newInput(wrongBoundary),
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-organization repository scope error = %v", err)
	}
	if _, err := store.ListRunners(ctx, fixture.member, organizationScope); !errors.Is(err, ErrForbidden) {
		t.Fatalf("organization member runner list error = %v", err)
	}
	if _, err := store.CreateRegistrationToken(ctx, fixture.owner, newInput(RunnerScope{
		UserID: fixture.owner.ID,
	})); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("dormant user scope error = %v", err)
	}
}

func newRunnerStoreFixture(t *testing.T, store *Store) runnerStoreFixture {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	fixture := runnerStoreFixture{
		store:           store,
		owner:           platformTestUser("runner-owner-" + suffix),
		maintainer:      platformTestUser("runner-maintainer-" + suffix),
		member:          platformTestUser("runner-member-" + suffix),
		repositoryAdmin: platformTestUser("runner-admin-" + suffix),
		organizationID:  uuid.NewString(),
		otherOrgID:      uuid.NewString(),
		repositoryID:    uuid.NewString(),
	}
	pool := store.pool
	for _, user := range []User{fixture.owner, fixture.maintainer, fixture.member, fixture.repositoryAdmin} {
		mustIdentityExec(t, pool, `
			INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
		`, user.ID, user.Username, user.DisplayName)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Runner organization', 'private', $3),
		       ($4, $5, 'Other runner organization', 'private', $3)
	`, fixture.organizationID, "runner-org-"+suffix, fixture.owner.ID,
		fixture.otherOrgID, "other-runner-org-"+suffix)
	for _, membership := range []struct {
		user User
		role string
	}{
		{fixture.owner, "owner"},
		{fixture.maintainer, "maintainer"},
		{fixture.member, "member"},
		{fixture.repositoryAdmin, "member"},
	} {
		mustIdentityExec(t, pool, `
			INSERT INTO organization_memberships (organization_id, user_id, role)
			VALUES ($1, $2, $3)
		`, fixture.organizationID, membership.user.ID, membership.role)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, fixture.otherOrgID, fixture.owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Runner repository', 'private', $4, $5, 'main', $6)
	`, fixture.repositoryID, fixture.organizationID, "runner-repo-"+suffix,
		canonicalTestLoreID(fixture.repositoryID), "lore://runner-repo-"+suffix, fixture.owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role)
		VALUES ($1, $2, 'admin')
	`, fixture.repositoryID, fixture.repositoryAdmin.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `
			DELETE FROM audit_events
			WHERE organization_id IN ($1, $2) OR actor_id IN ($3, $4, $5, $6)
		`, fixture.organizationID, fixture.otherOrgID, fixture.owner.ID, fixture.maintainer.ID,
			fixture.member.ID, fixture.repositoryAdmin.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id IN ($1, $2)`,
			fixture.organizationID, fixture.otherOrgID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2, $3, $4)`,
			fixture.owner.ID, fixture.maintainer.ID, fixture.member.ID, fixture.repositoryAdmin.ID)
	})
	return fixture
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
