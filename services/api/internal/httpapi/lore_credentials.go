package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/authz"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/loreauth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

var errScopedLoreCredentialUnavailable = errors.New("a scoped Lore credential is unavailable")

func (api *API) repositoryForRead(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.Repository, *platform.User, bool) {
	actor, ok := api.ResolveOptionalActor(writer, request)
	if !ok {
		return platform.Repository{}, nil, false
	}
	if reader, ok := api.store.(RepositoryReader); ok {
		repository, err := reader.RepositoryForRead(request.Context(), actor, request.PathValue("owner"),
			request.PathValue("repository"))
		if err != nil {
			api.platformError(writer, request, "get repository", err)
			return platform.Repository{}, nil, false
		}
		return repository, actor, true
	}
	repository, err := api.store.PublicRepository(request.Context(), request.PathValue("owner"),
		request.PathValue("repository"))
	if err != nil {
		api.platformError(writer, request, "get repository", err)
		return platform.Repository{}, nil, false
	}
	return repository, actor, true
}

func (api *API) scopedLoreCredential(
	ctx context.Context,
	actor platform.User,
	repository platform.Repository,
	requested []string,
) (loreclient.Credential, error) {
	if api.allowLegacyLoreIdentity && strings.TrimSpace(api.loreIdentity) != "" {
		return loreclient.Credential{Identity: api.loreIdentity}, nil
	}
	if api.loreAuth != nil && api.loreCredentialClient != nil {
		resourceID := "urc-" + repository.LoreRepositoryID
		token, err := api.loreAuth.IssueResourceToken(ctx, actor.ID, resourceID, requested)
		if err != nil {
			return loreclient.Credential{}, err
		}
		return loreclient.Credential{
			Token:    token,
			AuthURL:  api.loreAuth.AuthURL(),
			Identity: actor.ID,
		}, nil
	}
	if api.allowLegacyLoreIdentity && strings.TrimSpace(api.loreIdentity) != "" {
		return loreclient.Credential{Identity: api.loreIdentity}, nil
	}
	return loreclient.Credential{}, errScopedLoreCredentialUnavailable
}

func (api *API) listLoreBranches(
	ctx context.Context,
	actor platform.User,
	repository platform.Repository,
) ([]loreclient.Branch, error) {
	credential, err := api.scopedLoreCredential(ctx, actor, repository, []string{authz.PermissionRead})
	if err != nil {
		return nil, err
	}
	if credential.Token != "" {
		branches, err := api.loreCredentialClient.BranchesWithCredential(ctx, loreclient.RepositoryRef{
			CacheKey: repository.ID,
			URL:      repository.LoreURL,
		}, credential)
		if err != nil {
			return nil, errors.New("Lore branch lookup failed")
		}
		if err := api.observeBranchStates(ctx, repository, branches); err != nil {
			return nil, err
		}
		return branches, nil
	}
	branches, err := api.lore.Branches(ctx, loreclient.RepositoryRef{CacheKey: repository.ID, URL: repository.LoreURL},
		credential.Identity)
	if err != nil {
		return nil, err
	}
	if err := api.observeBranchStates(ctx, repository, branches); err != nil {
		return nil, err
	}
	return branches, nil
}

func (api *API) observeBranchStates(
	ctx context.Context,
	repository platform.Repository,
	branches []loreclient.Branch,
) error {
	observer, ok := api.store.(BranchStateObserver)
	if !ok {
		return nil
	}
	for _, branch := range branches {
		if branch.Archived || branch.LatestRevision == "" {
			continue
		}
		if err := observer.ObserveBranchState(ctx, repository.ID, branch.ID, branch.Name, branch.LatestRevision); err != nil {
			return errors.New("could not record the Lore branch state")
		}
	}
	return nil
}

func (api *API) repositoryInfoForRegistration(
	ctx context.Context,
	request *http.Request,
	actor platform.User,
	repositoryURL string,
) (loreclient.Repository, error) {
	if api.allowLegacyLoreIdentity && strings.TrimSpace(api.loreIdentity) != "" {
		return api.lore.RepositoryInfo(ctx, repositoryURL, api.loreIdentity)
	}
	if api.loreCredentialClient == nil {
		if api.allowLegacyLoreIdentity && strings.TrimSpace(api.loreIdentity) != "" {
			return api.lore.RepositoryInfo(ctx, repositoryURL, api.loreIdentity)
		}
		return loreclient.Repository{}, errScopedLoreCredentialUnavailable
	}
	rawToken := strings.TrimSpace(request.Header.Get("X-Lore-User-Token"))
	if rawToken == "" || api.loreAuth == nil {
		return loreclient.Repository{}, errors.New("a short-lived Lore user token is required")
	}
	claims, err := api.loreAuth.VerifyUserToken(rawToken)
	if err != nil || claims.Subject != actor.ID {
		return loreclient.Repository{}, errors.New("the Lore token does not belong to the current user")
	}
	info, err := api.loreCredentialClient.RepositoryInfoWithCredential(ctx, repositoryURL, loreclient.Credential{
		Token: rawToken, AuthURL: api.loreAuth.AuthURL(), Identity: actor.ID,
	})
	if err != nil {
		return loreclient.Repository{}, errors.New("Lore repository verification failed")
	}
	resourceID := "urc-" + info.ID
	permissions, found := loreResourcePermissions(claims, resourceID)
	if !found || !authz.RequirePermission(authz.OperationRepositoryCreate, permissions) {
		return loreclient.Repository{}, errors.New("the Lore token lacks repository administration scope")
	}
	return info, nil
}

func loreResourcePermissions(claims loreauth.LoreClaims, resourceID string) (map[string]bool, bool) {
	for _, resource := range claims.Resources {
		if resource.ResourceID == resourceID {
			permissions := make(map[string]bool, len(resource.Permission))
			for _, permission := range resource.Permission {
				permissions[permission] = true
			}
			return permissions, true
		}
	}
	return nil, false
}
