package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/authz"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/loreauth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

var errScopedLoreCredentialUnavailable = errors.New("a scoped Lore credential is unavailable")

type RepositoryReader interface {
	RepositoryForRead(
		context.Context, *platform.User, string, string,
	) (platform.Repository, error)
}

type BranchStateObserver interface {
	ObserveBranchState(context.Context, string, string, string, string) error
}

func publicLoreRepositoryURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path == "" ||
		strings.Count(parsed.Path, "/") != 1 || parsed.RawQuery != "" ||
		parsed.Fragment != "" || parsed.Scheme != "lores" {
		return "", errors.New("invalid Lore repository URL")
	}
	partition := strings.TrimPrefix(parsed.Path, "/")
	if len(partition) != 32 || strings.Trim(partition, "0123456789abcdef") != "" {
		return "", errors.New("Lore repository URL must contain one canonical partition")
	}
	return (&url.URL{Scheme: "lores", Host: parsed.Host, Path: "/" + partition}).String(), nil
}

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
	scope := loreclient.ScopeRead
	if requestedNeedsWrite(requested) {
		scope = loreclient.ScopeWrite
	}
	if api.allowLegacyLoreIdentity && strings.TrimSpace(api.loreIdentity) != "" {
		return loreclient.Credential{
			Partition:           repository.LoreRepositoryID,
			Scope:               scope,
			Identity:            api.loreIdentity,
			Principal:           loreclient.UserPrincipal(actor.ID),
			InsecureDevelopment: true,
		}, nil
	}
	if api.loreAuth == nil {
		return loreclient.Credential{}, errScopedLoreCredentialUnavailable
	}
	return api.loreAuth.IssueResourceToken(ctx, actor.ID, "urc-"+repository.LoreRepositoryID, requested)
}

func requestedNeedsWrite(requested []string) bool {
	for _, permission := range requested {
		if permission == authz.PermissionWrite || permission == authz.PermissionAdmin ||
			permission == authz.PermissionObliterate {
			return true
		}
	}
	return false
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
	branches, err := api.lore.Branches(ctx, loreclient.RepositoryRef{
		CacheKey: repository.ID, URL: repository.LoreURL, LoreRepositoryID: repository.LoreRepositoryID,
	}, credential)
	if err != nil {
		return nil, errors.New("Lore branch lookup failed")
	}
	if err := api.observeBranchStates(ctx, repository, branches); err != nil {
		return nil, err
	}
	return branches, nil
}

func (api *API) listPublicLoreBranches(
	ctx context.Context,
	repository platform.Repository,
) ([]loreclient.Branch, error) {
	if api.loreAuth == nil {
		return nil, errScopedLoreCredentialUnavailable
	}
	if api.serviceSubjects.PublicReader == "" {
		return nil, errScopedLoreCredentialUnavailable
	}
	credential, err := api.loreAuth.IssueServiceResourceToken(ctx,
		loreclient.ServicePrincipal(loreclient.ServicePurposePublicReader, api.serviceSubjects.PublicReader),
		"urc-"+repository.LoreRepositoryID, []string{authz.PermissionRead})
	if err != nil {
		return nil, errScopedLoreCredentialUnavailable
	}
	branches, err := api.lore.Branches(ctx, loreclient.RepositoryRef{
		CacheKey: repository.ID, URL: repository.LoreURL, LoreRepositoryID: repository.LoreRepositoryID,
	}, credential)
	if err != nil {
		return nil, errors.New("Lore branch lookup failed")
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
		if err := observer.ObserveBranchState(ctx, repository.ID, branch.ID, branch.Name,
			branch.LatestRevision); err != nil {
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
		return api.lore.RepositoryInfo(ctx, repositoryURL, loreclient.Credential{
			Partition: "legacy", Scope: loreclient.ScopeRead, Identity: api.loreIdentity,
			Principal: loreclient.UserPrincipal(actor.ID), InsecureDevelopment: true,
		})
	}
	if api.loreAuth == nil {
		return loreclient.Repository{}, errScopedLoreCredentialUnavailable
	}
	rawToken := strings.TrimSpace(request.Header.Get("X-Lore-User-Token"))
	if rawToken == "" {
		return loreclient.Repository{}, errors.New("a short-lived Lore user token is required")
	}
	claims, err := api.loreAuth.VerifyUserToken(rawToken)
	if err != nil || claims.Subject != actor.ID || claims.IsServiceAccount || len(claims.Resources) != 1 {
		return loreclient.Repository{}, errors.New("the Lore token does not belong to the current user")
	}
	resource := claims.Resources[0]
	partition := strings.TrimPrefix(resource.ResourceID, "urc-")
	if !containsPermission(resource.Permission, authz.PermissionAdmin) {
		return loreclient.Repository{}, errors.New("the Lore token lacks repository administration scope")
	}
	authenticationToken, authenticationExpiresAt, err := api.loreAuth.IssueAuthenticationToken(ctx, actor.ID)
	if err != nil {
		return loreclient.Repository{}, errors.New("could not issue the Lore authentication token")
	}
	credential := loreclient.Credential{
		Partition:               partition,
		Scope:                   loreclient.ScopeRead,
		ResourceID:              resource.ResourceID,
		Subject:                 claims.Subject,
		RequestedScopes:         []string{string(loreclient.ScopeWrite)},
		GrantedScopes:           []string{string(loreclient.ScopeWrite)},
		Identity:                claims.Subject,
		Token:                   rawToken,
		AuthenticationToken:     authenticationToken,
		AuthURL:                 api.loreAuth.AuthURL(),
		ExpiresAt:               claims.Expiry.Time(),
		AuthenticationExpiresAt: authenticationExpiresAt,
		Principal:               loreclient.UserPrincipal(actor.ID),
	}
	info, err := api.lore.RepositoryInfo(ctx, repositoryURL, credential)
	if err != nil || info.ID != partition {
		return loreclient.Repository{}, errors.New("Lore repository verification failed")
	}
	if _, err := api.loreAuth.AuthorizeUserToken(ctx, rawToken, resource.ResourceID,
		authz.PermissionAdmin); err != nil {
		return loreclient.Repository{}, errors.New("the Lore token lacks current repository administration scope")
	}
	return info, nil
}

func containsPermission(permissions []string, wanted string) bool {
	for _, permission := range permissions {
		if permission == wanted {
			return true
		}
	}
	return false
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
