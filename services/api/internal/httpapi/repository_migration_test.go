package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type migrationHTTPStore struct {
	fakeStore
	migration  platform.RepositoryMigration
	repository platform.Repository
	target     platform.LoreServer
	states     []string
	failedWith error
}

func (store *migrationHTTPStore) BeginRepositoryMigration(
	context.Context,
	platform.User,
	string,
	string,
	string,
) (platform.RepositoryMigration, platform.Repository, platform.LoreServer, error) {
	return store.migration, store.repository, store.target, nil
}

func (store *migrationHTTPStore) MarkRepositoryMigrationMirroring(context.Context, string) error {
	store.states = append(store.states, platform.RepositoryMigrationMirroring)
	return nil
}

func (store *migrationHTTPStore) MarkRepositoryMigrationRepointing(context.Context, string) error {
	store.states = append(store.states, platform.RepositoryMigrationRepointing)
	return nil
}

func (store *migrationHTTPStore) CompleteRepositoryMigration(context.Context, string) error {
	store.states = append(store.states, platform.RepositoryMigrationCompleted)
	store.migration.State = platform.RepositoryMigrationCompleted
	store.repository.MigratingAt = nil
	return nil
}

func (store *migrationHTTPStore) FailRepositoryMigration(
	_ context.Context,
	_ string,
	failure error,
) error {
	store.states = append(store.states, platform.RepositoryMigrationFailed)
	store.migration.State = platform.RepositoryMigrationFailed
	store.repository.MigratingAt = nil
	store.failedWith = failure
	return nil
}

func (store *migrationHTTPStore) ListRepositoryMigrations(
	context.Context,
	string,
	string,
) ([]platform.RepositoryMigration, error) {
	return []platform.RepositoryMigration{store.migration}, nil
}

type migrationHTTPLore struct {
	fakeLore
	err   error
	input loreclient.RepositoryMirrorInput
}

func (client *migrationHTTPLore) MirrorRepository(
	_ context.Context,
	input loreclient.RepositoryMirrorInput,
) error {
	client.input = input
	return client.err
}

type migrationCredentials struct{}

func (migrationCredentials) ForRepository(
	_ context.Context,
	request loreclient.CredentialRequest,
) (loreclient.Credential, error) {
	return loreclient.Credential{
		Partition: request.Partition, Scope: request.Scope, Identity: "migration",
		Principal: request.Principal, InsecureDevelopment: true,
	}, nil
}

func migrationFixture() (platform.RepositoryMigration, platform.Repository, platform.LoreServer) {
	migratingAt := time.Now().UTC()
	repository := platform.Repository{
		ID: "repository-1", DisplayName: "Lore", Description: "A Lore repository",
		LoreRepositoryID: "0123456789abcdef0123456789abcdef", LoreURL: "lores://source/0123456789abcdef0123456789abcdef",
		DefaultBranch: "main", MigratingAt: &migratingAt,
	}
	migration := platform.RepositoryMigration{
		ID: "migration-1", RepositoryID: repository.ID, FromServerID: "server-1", ToServerID: "server-2",
		State: platform.RepositoryMigrationPending,
	}
	target := platform.LoreServer{ID: "server-2", PublicURL: "lores://target"}
	return migration, repository, target
}

func TestRepositoryMigrationStateTransitions(t *testing.T) {
	migration, repository, target := migrationFixture()
	store := &migrationHTTPStore{
		migration: migration, repository: repository, target: target,
	}
	lore := &migrationHTTPLore{}
	api := &API{
		lore: lore, loreCredentials: migrationCredentials{},
		serviceSubjects: loreclient.ServiceSubjects{RepositoryRegistration: "migration-service"},
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	api.runRepositoryMigration(store, migration, repository, target)

	want := []string{
		platform.RepositoryMigrationMirroring,
		platform.RepositoryMigrationRepointing,
		platform.RepositoryMigrationCompleted,
	}
	if len(store.states) != len(want) {
		t.Fatalf("migration states = %v, want %v", store.states, want)
	}
	for index := range want {
		if store.states[index] != want[index] {
			t.Fatalf("migration state %d = %q, want %q", index, store.states[index], want[index])
		}
	}
	if lore.input.Target.URL != target.PublicURL+"/"+repository.LoreRepositoryID {
		t.Fatalf("target Lore URL = %q", lore.input.Target.URL)
	}
	if lore.input.SourceCredential.Scope != loreclient.ScopeRead ||
		lore.input.TargetCredential.Scope != loreclient.ScopeAdmin {
		t.Fatalf("migration credential scopes = %q/%q", lore.input.SourceCredential.Scope,
			lore.input.TargetCredential.Scope)
	}
	if lore.input.TargetCredential.Principal.ServicePurpose != loreclient.ServicePurposeRepositoryRegistration {
		t.Fatalf("migration principal purpose = %q", lore.input.TargetCredential.Principal.ServicePurpose)
	}
	if store.failedWith != nil {
		t.Fatalf("successful migration recorded failure: %v", store.failedWith)
	}
}

func TestRepositoryMigrationFailureLiftsReadOnly(t *testing.T) {
	migration, repository, target := migrationFixture()
	store := &migrationHTTPStore{
		migration: migration, repository: repository, target: target,
	}
	lore := &migrationHTTPLore{err: errors.New("mirror failed")}
	api := &API{
		lore: lore, loreCredentials: migrationCredentials{},
		serviceSubjects: loreclient.ServiceSubjects{RepositoryRegistration: "migration-service"},
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	api.runRepositoryMigration(store, migration, repository, target)

	if len(store.states) != 2 || store.states[0] != platform.RepositoryMigrationMirroring ||
		store.states[1] != platform.RepositoryMigrationFailed {
		t.Fatalf("failed migration states = %v", store.states)
	}
	if store.repository.MigratingAt != nil {
		t.Fatal("failed migration left the repository read-only")
	}
	if store.repository.LoreURL != repository.LoreURL {
		t.Fatal("failed migration changed the source Lore URL")
	}
	if store.failedWith == nil || store.failedWith.Error() == "" {
		t.Fatal("failed migration did not retain the mirror error")
	}
}

func TestRepositoryMigrationAdminGuard(t *testing.T) {
	codec, err := auth.NewSecretCodec("repository migration guard secret")
	if err != nil {
		t.Fatal(err)
	}
	authenticationStore := &fakeAuthenticationStore{}
	cookie, _ := prepareSessionCookie(t, authenticationStore, codec)
	migration, repository, target := migrationFixture()
	store := &migrationHTTPStore{
		fakeStore: fakeStore{user: platform.User{ID: "user-1", Username: "alice"}},
		migration: migration, repository: repository, target: target,
	}
	handler := New(
		store, fakeLore{}, auth.DisabledAuthenticator{}, healthy{}, "",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithAuthentication(AuthOptions{
			SessionStore: authenticationStore, Secrets: codec,
			PublicOrigin:  "https://app.example",
			SessionCookie: SessionCookieOptions{Name: "lorehub_session", Path: "/"},
		}),
		WithInstanceAdminUsernames([]string{"bob"}),
	)
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/admin/repositories/acme/lore/migrations", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin migration status = %d, want 403", response.Code)
	}
}
