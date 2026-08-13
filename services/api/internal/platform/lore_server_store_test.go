package platform

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/auth"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

func TestLoreTransportResolverUsesInstanceInternalAndRegisteredPublicAuthorities(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	organizationID := uuid.NewString()
	ownerID := uuid.NewString()
	mustIdentityExec(t, pool, `
		INSERT INTO users (id, username, display_name) VALUES ($1, $2, 'Lore transport owner')
	`, ownerID, "lore-transport-owner-"+suffix)
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Lore transport resolver', 'private', $3)
	`, organizationID, "lore-transport-"+suffix, ownerID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, organizationID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ownerID)
	})

	instancePublicURL := "lores://instance-transport-" + suffix + ".example:41337"
	instance, err := store.EnsureInstanceLoreServer(ctx, instancePublicURL)
	if err != nil {
		t.Fatalf("ensure instance Lore server: %v", err)
	}
	registered := insertLoreServerFixture(t, pool, organizationID, "registered-transport-"+suffix)
	instanceInternalURL := "lores://internal-transport-" + suffix + ".example:41337"
	resolver, err := NewLoreTransportResolver(store, instanceInternalURL)
	if err != nil {
		t.Fatal(err)
	}
	partition := "0123456789abcdef0123456789abcdef"
	instanceTransport, err := resolver.ResolveTransport(ctx, instancePublicURL+"/"+partition)
	if err != nil {
		t.Fatal(err)
	}
	if instanceTransport.ServerID != instance.ID || instanceTransport.Authority != instanceInternalURL {
		t.Fatalf("instance transport = %+v", instanceTransport)
	}
	registeredTransport, err := resolver.ResolveTransport(ctx, registered.PublicURL+"/"+partition)
	if err != nil {
		t.Fatal(err)
	}
	if registeredTransport.ServerID != registered.ID || registeredTransport.Authority != registered.PublicURL {
		t.Fatalf("registered transport = %+v", registeredTransport)
	}
	mustIdentityExec(t, pool, `
		UPDATE lore_servers SET status = 'revoked', revoked_at = now() WHERE id = $1
	`, registered.ID)
	_, err = resolver.ResolveTransport(ctx, registered.PublicURL+"/"+partition)
	if !errors.Is(err, loreclient.ErrUnknownServerAuthority) {
		t.Fatalf("revoked Lore server transport error = %v", err)
	}
	_, err = resolver.ResolveTransport(ctx, "lores://unknown-"+suffix+".example:41337/"+partition)
	var unknownAuthority *loreclient.UnknownServerAuthorityError
	if !errors.Is(err, loreclient.ErrUnknownServerAuthority) || !errors.As(err, &unknownAuthority) {
		t.Fatalf("unknown transport authority error = %v", err)
	}
}

func TestLoreServerResolverPrecedenceAndEntitlement(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	owner := platformTestUser("lore-server-owner-" + suffix)
	organizationID := uuid.NewString()
	organizationSlug := "lore-server-" + suffix
	mustIdentityExec(t, pool, `
		INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
	`, owner.ID, owner.Username, owner.DisplayName)
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Lore server resolver', 'private', $3)
	`, organizationID, organizationSlug, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, organizationID, owner.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, organizationID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, owner.ID)
	})

	instance, err := store.EnsureInstanceLoreServer(ctx, "lores://instance-"+suffix+".example:41337")
	if err != nil {
		t.Fatalf("ensure instance Lore server: %v", err)
	}
	_, err = store.ResolveServerForNewRepository(ctx, organizationID, "")
	if selectionReason(err) != LoreServerSelectionEntitlementRequired {
		t.Fatalf("unentitled fallback error = %v, reason=%q", err, selectionReason(err))
	}

	serverA := insertLoreServerFixture(t, pool, organizationID, "server-a-"+suffix)
	serverB := insertLoreServerFixture(t, pool, organizationID, "server-b-"+suffix)
	explicit, err := store.ResolveServerForNewRepository(ctx, organizationID, serverA.ID)
	if err != nil || explicit.ID != serverA.ID {
		t.Fatalf("explicit selection = %+v, error=%v", explicit, err)
	}
	if _, err := store.SetOrganizationDefaultServer(ctx, owner, organizationSlug, serverB.ID); err != nil {
		t.Fatalf("set organization default: %v", err)
	}
	selected, err := store.ResolveServerForNewRepository(ctx, organizationID, "")
	if err != nil || selected.ID != serverB.ID {
		t.Fatalf("default selection = %+v, error=%v", selected, err)
	}
	selected, err = store.ResolveServerForNewRepository(ctx, organizationID, serverA.ID)
	if err != nil || selected.ID != serverA.ID {
		t.Fatalf("explicit selection did not override default: %+v, error=%v", selected, err)
	}

	if _, err := store.SetOrganizationDefaultServer(ctx, owner, organizationSlug, ""); err != nil {
		t.Fatalf("clear organization default: %v", err)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO entitlements (organization_id, feature, granted_by, grant_source)
		VALUES ($1, 'hosted_lore_server', $2, 'admin')
	`, organizationID, owner.ID)
	selected, err = store.ResolveServerForNewRepository(ctx, organizationID, "")
	if err != nil || selected.ID != instance.ID {
		t.Fatalf("entitled instance fallback = %+v, error=%v", selected, err)
	}

	otherOrganizationID := uuid.NewString()
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Other Lore server organization', 'private', $3)
	`, otherOrganizationID, "other-lore-server-"+suffix, owner.ID)
	otherServer := insertLoreServerFixture(t, pool, otherOrganizationID, "other-server-"+suffix)
	_, err = store.ResolveServerForNewRepository(ctx, organizationID, otherServer.ID)
	if selectionReason(err) != LoreServerSelectionExplicitUnavailable {
		t.Fatalf("cross-organization explicit selection error = %v, reason=%q", err, selectionReason(err))
	}
	_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, otherOrganizationID)
}

func TestLoreServerRegistrationTokenConsumptionIsAtomic(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	owner := platformTestUser("lore-token-owner-" + suffix)
	organizationID := uuid.NewString()
	organizationSlug := "lore-token-" + suffix
	mustIdentityExec(t, pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)`,
		owner.ID, owner.Username, owner.DisplayName)
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Lore registration token', 'private', $3)
	`, organizationID, organizationSlug, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, organizationID, owner.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, organizationID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, owner.ID)
	})
	secrets, err := auth.NewSecretCodec("atomic Lore server registration token secret")
	if err != nil {
		t.Fatal(err)
	}
	_, digest, err := auth.NewLoreServerRegistrationToken(secrets)
	if err != nil {
		t.Fatal(err)
	}
	input := CreateLoreServerRegistrationTokenInput{
		Digest: digest, ExpiresAt: time.Now().UTC().Add(30 * time.Minute),
	}
	if _, err := store.CreateLoreServerRegistrationToken(ctx, owner, organizationSlug, input); err != nil {
		t.Fatalf("create registration token: %v", err)
	}

	start := make(chan struct{})
	errorsByAttempt := make([]error, 2)
	var wait sync.WaitGroup
	for attempt := range errorsByAttempt {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			_, errorsByAttempt[index] = store.ConsumeLoreServerRegistrationToken(ctx, digest, time.Now().UTC())
		}(attempt)
	}
	close(start)
	wait.Wait()
	successes := 0
	rejections := 0
	for _, err := range errorsByAttempt {
		if err == nil {
			successes++
		} else if errors.Is(err, auth.ErrInvalidLoreServerRegistrationToken) {
			rejections++
		} else {
			t.Fatalf("unexpected consume error: %v", err)
		}
	}
	if successes != 1 || rejections != 1 {
		t.Fatalf("atomic consume results: successes=%d rejections=%d", successes, rejections)
	}
}

func TestLoreServerRegistrationAuthenticationHeartbeatAndRevocation(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	owner := platformTestUser("lore-register-owner-" + suffix)
	organizationID := uuid.NewString()
	organizationSlug := "lore-register-" + suffix
	mustIdentityExec(t, pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)`,
		owner.ID, owner.Username, owner.DisplayName)
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Lore server registration', 'private', $3)
	`, organizationID, organizationSlug, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, organizationID, owner.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, organizationID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, owner.ID)
	})

	secrets, err := auth.NewSecretCodec("Lore server registration and authentication secret")
	if err != nil {
		t.Fatal(err)
	}
	registration, registrationDigest, err := auth.NewLoreServerRegistrationToken(secrets)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateLoreServerRegistrationToken(ctx, owner, organizationSlug,
		CreateLoreServerRegistrationTokenInput{
			Digest: registrationDigest, ExpiresAt: time.Now().UTC().Add(30 * time.Minute),
		}); err != nil {
		t.Fatalf("create registration token: %v", err)
	}
	credential, credentialDigest, err := auth.NewLoreServerCredential(secrets)
	if err != nil {
		t.Fatal(err)
	}
	server, err := store.RegisterServer(ctx, secrets.Digest(registration), RegisterLoreServerInput{
		Name: "Primary storage", PublicURL: "lores://registered-" + suffix + ".example:41337",
		CredentialDigest: credentialDigest, CredentialKeyID: "test-lores-v1",
		CredentialExpiresAt: time.Now().UTC().Add(24 * time.Hour), LoreBuildVersion: "0.8.6",
		HookModuleVersion: "1.0.0", HealthMetadata: map[string]any{"state": "ready"},
	})
	if err != nil {
		t.Fatalf("register Lore server: %v", err)
	}
	if server.OrganizationID == nil || *server.OrganizationID != organizationID ||
		server.HealthMetadata["hookModuleVersion"] != "1.0.0" {
		t.Fatalf("registered Lore server = %+v", server)
	}
	authenticated, err := store.AuthenticateServer(ctx, secrets.Digest(credential), "test-lores-v1", time.Now().UTC())
	if err != nil || authenticated.ID != server.ID {
		t.Fatalf("authenticate Lore server = %+v, error=%v", authenticated, err)
	}
	seenAt := time.Now().UTC()
	if err := store.UpdateServerHealth(ctx, server.ID, seenAt, "0.8.7", "1.0.1",
		map[string]any{"state": "healthy"}); err != nil {
		t.Fatalf("update Lore server health: %v", err)
	}
	if err := store.RevokeServer(ctx, owner, organizationSlug, server.ID); err != nil {
		t.Fatalf("revoke Lore server: %v", err)
	}
	_, err = store.AuthenticateServer(
		ctx, secrets.Digest(credential), "test-lores-v1", time.Now().UTC(),
	)
	if !errors.Is(err, auth.ErrInvalidLoreServerCredential) {
		t.Fatalf("revoked Lore server credential error = %v", err)
	}
}

func TestLoreServerURLValidationRejectsPrivateAndReservedLiterals(t *testing.T) {
	for _, value := range []string{
		"lores://127.0.0.1:41337",
		"lores://10.20.30.40:41337",
		"lores://[::1]:41337",
		"lores://203.0.113.10:41337",
	} {
		if _, err := validateLoreServerURL(value, false); err == nil {
			t.Errorf("accepted restricted Lore server URL %q", value)
		}
	}
	normalized, err := validateLoreServerURL("lores://Lore.EXAMPLE:41337", false)
	if err != nil || normalized != "lores://lore.example:41337" {
		t.Fatalf("public Lore server URL = %q, error=%v", normalized, err)
	}
	if _, err := validateLoreServerURL("lores://10.20.30.40:41337", true); err != nil {
		t.Fatalf("explicit private-server allowance was ignored: %v", err)
	}
	for _, value := range []string{
		"lore://lore.example:41337",
		"lores://user@lore.example:41337",
		"lores://lore.example:41337/partition",
		"lores://lore.example:0",
	} {
		if _, err := validateLoreServerURL(value, false); err == nil {
			t.Errorf("accepted malformed Lore server URL %q", value)
		}
	}
}

func TestRepositoryImportRequiresMatchingRegisteredServerAuthority(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	owner := platformTestUser("lore-import-owner-" + suffix)
	organizationID := uuid.NewString()
	organizationSlug := "lore-import-" + suffix
	mustIdentityExec(t, pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)`,
		owner.ID, owner.Username, owner.DisplayName)
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Lore import authority', 'private', $3)
	`, organizationID, organizationSlug, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, organizationID, owner.ID)
	server := insertLoreServerFixture(t, pool, organizationID, "import-server-"+suffix)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, organizationID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, owner.ID)
	})
	matchingURL := server.PublicURL + "/0123456789abcdef0123456789abcdef"
	if err := store.ValidateRepositoryImportServer(ctx, owner, organizationSlug, server.ID, matchingURL); err != nil {
		t.Fatalf("matching import authority was rejected: %v", err)
	}
	if err := store.ValidateRepositoryImportServer(ctx, owner, organizationSlug, server.ID,
		"lores://different.example:41337/0123456789abcdef0123456789abcdef"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("mismatched import authority error = %v, want invalid input", err)
	}
}

func insertLoreServerFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	organizationID string,
	hostname string,
) LoreServer {
	t.Helper()
	server := LoreServer{
		ID: uuid.NewString(), PublicURL: "lores://" + hostname + ".example:41337", Name: hostname,
	}
	digest := sha256.Sum256([]byte(server.ID))
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO lore_servers (
			id, organization_id, name, public_url, status,
			credential_digest, credential_key_id, credential_expires_at
		) VALUES ($1, $2, $3, $4, 'active', $5, 'test-lores-v1', now() + interval '1 day')
	`, server.ID, organizationID, server.Name, server.PublicURL, digest[:]); err != nil {
		t.Fatalf("insert Lore server fixture: %v", err)
	}
	return server
}

func selectionReason(err error) string {
	var selectionError *LoreServerSelectionError
	if errors.As(err, &selectionError) {
		return selectionError.Reason
	}
	return ""
}
