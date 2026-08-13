package httpapi

import (
	"errors"
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/authz"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type repositoryRequest struct {
	Slug          string `json:"slug"`
	DisplayName   string `json:"displayName"`
	Description   string `json:"description"`
	Visibility    string `json:"visibility"`
	LoreURL       string `json:"loreUrl"`
	LoreServerID  string `json:"loreServerId"`
	DefaultBranch string `json:"defaultBranch"`
}

func (api *API) importRepository(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input repositoryRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if !validVisibility(input.Visibility) || input.LoreURL == "" || input.LoreServerID == "" ||
		len(input.Description) > 10_000 {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Repository import fields are invalid")
		return
	}
	publicLoreURL, err := publicLoreRepositoryURL(input.LoreURL)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_input",
			"The Lore URL must be a fixed repository endpoint")
		return
	}
	loreServers, supported := api.store.(LoreServerStore)
	if !supported {
		writeProblem(writer, http.StatusServiceUnavailable, "lore_servers_unavailable",
			"Lore server settings are unavailable")
		return
	}
	if err := loreServers.ValidateRepositoryImportServer(request.Context(), actor,
		request.PathValue("organization"), input.LoreServerID, publicLoreURL); err != nil {
		api.loreServerError(writer, request, "validate repository import Lore server", err)
		return
	}
	loreRepository, err := api.repositoryInfoForRegistration(request.Context(), request, actor, publicLoreURL)
	if err != nil {
		writeProblem(writer, http.StatusBadGateway, "lore_unavailable", "Lore repository could not be verified")
		return
	}
	if input.DisplayName == "" {
		input.DisplayName = loreRepository.Name
	}
	if input.Description == "" {
		input.Description = loreRepository.Description
	}
	repository, err := api.store.RegisterRepository(request.Context(), actor,
		request.PathValue("organization"), platform.RegisterRepositoryInput{
			Slug: input.Slug, DisplayName: input.DisplayName, Description: input.Description,
			Visibility: input.Visibility, LoreRepositoryID: loreRepository.ID, LoreURL: publicLoreURL,
			LoreServerID: input.LoreServerID, DefaultBranch: loreRepository.DefaultBranch,
		})
	if err != nil {
		api.platformError(writer, request, "import repository", err)
		return
	}
	writeJSON(writer, http.StatusCreated, repository)
}

func (api *API) retryRepositoryProvisioning(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	provisioner, supported := api.store.(repositoryProvisioningStore)
	if !supported || api.managedLoreClient == nil || api.loreAuth == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "provisioning_unavailable",
			"Lore repository provisioning is unavailable")
		return
	}
	repository, err := provisioner.RepositoryForProvisioning(request.Context(), actor,
		request.PathValue("owner"), request.PathValue("repository"))
	if err != nil {
		api.platformError(writer, request, "find repository provisioning record", err)
		return
	}
	if err := api.provisionManagedRepository(request, actor, repository, provisioner); err != nil {
		api.logger.Error("retry Lore repository provisioning", "error", err,
			"repository_id", repository.ID, "lore_repository_id", repository.LoreRepositoryID)
		writeProblem(writer, http.StatusBadGateway, "lore_unavailable", "Lore repository provisioning failed")
		return
	}
	repository.LifecycleState = "active"
	repository.ProvisioningError = ""
	writeJSON(writer, http.StatusOK, repository)
}

func (api *API) provisionManagedRepository(
	request *http.Request,
	actor platform.User,
	repository platform.Repository,
	provisioner repositoryProvisioningStore,
) error {
	resourceID := "urc-" + repository.LoreRepositoryID
	credential, err := api.loreAuth.IssueResourceToken(request.Context(), actor.ID, resourceID,
		[]string{authz.PermissionAdmin})
	if err != nil {
		return errors.New("could not issue the exact actor provisioning credential")
	}
	creationErr := api.managedLoreClient.CreateRepositoryWithCredential(request.Context(), repository.LoreURL,
		repository.LoreRepositoryID, repository.DisplayName, repository.Description, credential)
	info, verificationErr := api.lore.RepositoryInfo(request.Context(), repository.LoreURL, credential)
	if verificationErr == nil && info.ID == repository.LoreRepositoryID {
		return provisioner.MarkRepositoryProvisioned(request.Context(), actor, repository.ID)
	}
	if verificationErr == nil {
		verificationErr = errors.New("Lore returned a different repository ID")
	}
	if api.serviceSubjects.RepositoryRegistration == "" {
		return errors.New("repository provisioning service principal is not configured")
	}
	serviceCredential, serviceCredentialErr := api.loreAuth.IssueServiceResourceToken(request.Context(),
		loreclient.ServicePrincipal(loreclient.ServicePurposeRepositoryRegistration,
			api.serviceSubjects.RepositoryRegistration), resourceID, []string{authz.PermissionAdmin})
	if serviceCredentialErr == nil {
		info, infoErr := api.lore.RepositoryInfo(request.Context(), repository.LoreURL, serviceCredential)
		if infoErr == nil && info.ID == repository.LoreRepositoryID {
			return provisioner.MarkRepositoryProvisioned(request.Context(), actor, repository.ID)
		}
		if infoErr != nil {
			serviceCredentialErr = infoErr
		} else {
			serviceCredentialErr = errors.New("Lore returned a different repository ID")
		}
	}
	if failErr := provisioner.MarkRepositoryProvisioningFailed(request.Context(), actor, repository.ID,
		"Lore repository creation was rejected"); failErr != nil {
		return errors.New("Lore repository provisioning failed")
	}
	return errors.Join(errors.New("Lore repository creation was rejected"), creationErr, verificationErr,
		serviceCredentialErr)
}
