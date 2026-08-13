package httpapi

import (
	"net/http"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/auth"
)

type accountResponse struct {
	User  accountUser           `json:"user"`
	Token *accountTokenResponse `json:"token,omitempty"`
}

type accountUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl"`
}

type accountTokenResponse struct {
	ID          string     `json:"id"`
	Prefix      string     `json:"prefix"`
	Permissions []string   `json:"permissions"`
	ExpiresAt   time.Time  `json:"expiresAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt"`
}

func (api *API) account(writer http.ResponseWriter, request *http.Request) {
	user, principal, ok := api.resolveActor(writer, request)
	if !ok {
		return
	}

	response := accountResponse{User: accountUser{
		ID:          user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		AvatarURL:   user.AvatarURL,
	}}
	if principal.CredentialKind == auth.CredentialPersonalAccessToken {
		response.Token = &accountTokenResponse{
			ID:          principal.CredentialID,
			Prefix:      principal.CredentialPrefix,
			Permissions: append([]string(nil), principal.Scopes...),
			ExpiresAt:   principal.CredentialExpiresAt,
			LastUsedAt:  principal.CredentialLastUsedAt,
		}
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusOK, response)
}
