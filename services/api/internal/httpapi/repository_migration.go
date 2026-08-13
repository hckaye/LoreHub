package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const repositoryMigrationTimeout = 10 * time.Minute

type repositoryMigrationStore interface {
	BeginRepositoryMigration(
		context.Context, platform.User, string, string, string,
	) (platform.RepositoryMigration, platform.Repository, platform.LoreServer, error)
	MarkRepositoryMigrationMirroring(context.Context, string) error
	MarkRepositoryMigrationRepointing(context.Context, string) error
	CompleteRepositoryMigration(context.Context, string) error
	FailRepositoryMigration(context.Context, string, error) error
	ListRepositoryMigrations(context.Context, string, string) ([]platform.RepositoryMigration, error)
}

type repositoryMigrationRequest struct {
	TargetServerID string `json:"targetServerId"`
}

func (api *API) migrateRepository(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "private, no-store")
	store, ok := api.store.(repositoryMigrationStore)
	if !ok {
		writeProblem(writer, http.StatusServiceUnavailable, "migration_unavailable",
			"Repository migration is unavailable")
		return
	}
	var input repositoryMigrationRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	input.TargetServerID = strings.TrimSpace(input.TargetServerID)
	if input.TargetServerID == "" {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "A target Lore server is required")
		return
	}
	actor := instanceAdminActor(request)
	migration, repository, target, err := store.BeginRepositoryMigration(request.Context(), actor,
		request.PathValue("owner"), request.PathValue("repository"), input.TargetServerID)
	if err != nil {
		api.platformError(writer, request, "start repository migration", err)
		return
	}
	go api.runRepositoryMigration(store, migration, repository, target)
	writeJSON(writer, http.StatusAccepted, migration)
}

func (api *API) listRepositoryMigrations(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "private, no-store")
	store, ok := api.store.(repositoryMigrationStore)
	if !ok {
		writeProblem(writer, http.StatusServiceUnavailable, "migration_unavailable",
			"Repository migration is unavailable")
		return
	}
	migrations, err := store.ListRepositoryMigrations(request.Context(), request.PathValue("owner"),
		request.PathValue("repository"))
	if err != nil {
		api.platformError(writer, request, "list repository migrations", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"migrations": migrations})
}

func (api *API) runRepositoryMigration(
	store repositoryMigrationStore,
	migration platform.RepositoryMigration,
	repository platform.Repository,
	target platform.LoreServer,
) {
	ctx, cancel := context.WithTimeout(context.Background(), repositoryMigrationTimeout)
	defer cancel()
	fail := func(err error) {
		failureContext, failureCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer failureCancel()
		if failErr := store.FailRepositoryMigration(failureContext, migration.ID, err); failErr != nil {
			api.logMigrationError("record repository migration failure", migration, failErr)
		}
	}
	if err := store.MarkRepositoryMigrationMirroring(ctx, migration.ID); err != nil {
		api.logMigrationError("mark repository migration as mirroring", migration, err)
		fail(err)
		return
	}
	if api.loreCredentials == nil {
		err := errors.New("Lore migration credentials are unavailable")
		fail(err)
		return
	}
	mirror, ok := api.lore.(loreclient.RepositoryMigrationClient)
	if !ok {
		err := errors.New("Lore migration client is unavailable")
		fail(err)
		return
	}
	targetURL := strings.TrimRight(target.PublicURL, "/") + "/" + repository.LoreRepositoryID
	sourceRef := loreclient.RepositoryRef{
		CacheKey: repository.ID, URL: repository.LoreURL,
		LoreRepositoryID: repository.LoreRepositoryID, DefaultBranch: repository.DefaultBranch,
	}
	targetRef := loreclient.RepositoryRef{
		CacheKey: repository.ID, URL: targetURL,
		LoreRepositoryID: repository.LoreRepositoryID, DefaultBranch: repository.DefaultBranch,
	}
	if api.serviceSubjects.RepositoryRegistration == "" {
		fail(errors.New("repository migration service principal is not configured"))
		return
	}
	principal := loreclient.ServicePrincipal(loreclient.ServicePurposeRepositoryRegistration,
		api.serviceSubjects.RepositoryRegistration)
	sourceCredential, err := api.loreCredential(ctx, sourceRef, principal, loreclient.ScopeRead)
	if err != nil {
		fail(fmt.Errorf("issue source migration credential: %w", err))
		return
	}
	targetCredential, err := api.loreCredential(ctx, targetRef, principal, loreclient.ScopeAdmin)
	if err != nil {
		fail(fmt.Errorf("issue target migration credential: %w", err))
		return
	}
	if err := mirror.MirrorRepository(ctx, loreclient.RepositoryMirrorInput{
		Source: sourceRef, Target: targetRef, Name: repository.DisplayName,
		Description: repository.Description, SourceCredential: sourceCredential,
		TargetCredential: targetCredential,
	}); err != nil {
		fail(fmt.Errorf("mirror repository data: %w", err))
		return
	}
	if err := store.MarkRepositoryMigrationRepointing(ctx, migration.ID); err != nil {
		fail(err)
		return
	}
	if err := store.CompleteRepositoryMigration(ctx, migration.ID); err != nil {
		fail(err)
	}
}

func (api *API) logMigrationError(operation string, migration platform.RepositoryMigration, err error) {
	if api.logger != nil {
		api.logger.Error(operation, "error", err, "migration_id", migration.ID,
			"repository_id", migration.RepositoryID)
	}
}
