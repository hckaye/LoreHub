package lore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type Scope string

const (
	ScopeRead  Scope = "repository:read"
	ScopeWrite Scope = "repository:write"
)

const (
	ServicePurposePublicReader           = "public-reader"
	ServicePurposeActionsRunner          = "actions-runner"
	ServicePurposeRepositoryRegistration = "repository-registration"
)

var (
	ErrCredentialUnavailable = errors.New("Lore repository credential is unavailable")
	ErrInvalidPrincipal      = errors.New("Lore credential principal is invalid")
	ErrLoreAuthentication    = errors.New("Lore credential authentication failed")
)

// Principal identifies the caller that the control plane authorized. Exactly one
// of UserID and ServicePurpose must be set; bearer tokens are never principals.
type Principal struct {
	UserID         string
	ServicePurpose string
}

func UserPrincipal(userID string) Principal {
	return Principal{UserID: strings.TrimSpace(userID)}
}

func ServicePrincipal(purpose string) Principal {
	return Principal{ServicePurpose: strings.TrimSpace(purpose)}
}

func (principal Principal) valid() bool {
	return (principal.UserID != "") != (principal.ServicePurpose != "")
}

func (principal Principal) equal(other Principal) bool {
	return principal.UserID == other.UserID && principal.ServicePurpose == other.ServicePurpose
}

// CredentialMaterial is the configured, short-lived credential for one Lore
// partition. Token and AuthURL are intentionally kept separate and are never
// included in errors or log fields.
type CredentialMaterial struct {
	Identity string `json:"identity"`
	Token    string `json:"token"`
	AuthURL  string `json:"authUrl"`
}

type CredentialRequest struct {
	Principal  Principal
	Repository RepositoryRef
	Scope      Scope
}

type Credential struct {
	Partition           string    `json:"partition,omitempty"`
	Scope               Scope     `json:"scope,omitempty"`
	Identity            string    `json:"-"`
	Token               string    `json:"-"`
	AuthURL             string    `json:"-"`
	Principal           Principal `json:"principal"`
	InsecureDevelopment bool      `json:"insecureDevelopment,omitempty"`
}

type CredentialProvider interface {
	ForRepository(context.Context, CredentialRequest) (Credential, error)
}

type configuredCredentialProvider struct {
	environment         string
	materials           map[string]CredentialMaterial
	developmentIdentity string
	allowDevelopment    bool
}

func NewCredentialProvider(
	environment string,
	materials map[string]CredentialMaterial,
	developmentIdentity string,
	allowDevelopmentFallback bool,
) (CredentialProvider, error) {
	if environment == "" {
		environment = "production"
	}
	clean := make(map[string]CredentialMaterial, len(materials))
	for partition, material := range materials {
		partition = strings.TrimSpace(partition)
		material.Identity = strings.TrimSpace(material.Identity)
		material.Token = strings.TrimSpace(material.Token)
		material.AuthURL = strings.TrimSpace(material.AuthURL)
		if partition == "" {
			return nil, errors.New("Lore credentials require non-empty partitions")
		}
		if environment != "development" && environment != "test" {
			if err := validateProductionMaterial(material); err != nil {
				return nil, err
			}
		}
		clean[partition] = material
	}
	if environment != "development" && environment != "test" && allowDevelopmentFallback {
		return nil, errors.New("development Lore credential fallback is not allowed outside development or test")
	}
	if environment != "development" && environment != "test" && strings.TrimSpace(developmentIdentity) != "" {
		return nil, errors.New("identity-only Lore credential fallback is not allowed in production")
	}
	return configuredCredentialProvider{
		environment:         environment,
		materials:           clean,
		developmentIdentity: strings.TrimSpace(developmentIdentity),
		allowDevelopment:    allowDevelopmentFallback,
	}, nil
}

func NewDevelopmentCredentialProvider(identity string) CredentialProvider {
	provider, _ := NewCredentialProvider("development", nil, identity, true)
	return provider
}

func (provider configuredCredentialProvider) ForRepository(
	ctx context.Context,
	request CredentialRequest,
) (Credential, error) {
	if err := ctx.Err(); err != nil {
		return Credential{}, err
	}
	if err := validateCredentialRequest(request); err != nil {
		return Credential{}, err
	}
	partition := request.Repository.CanonicalPartition()
	if material, ok := provider.materials[partition]; ok {
		insecureDevelopment := (provider.environment == "development" || provider.environment == "test") &&
			(material.Token == "" || material.AuthURL == "")
		credential := Credential{
			Partition:           partition,
			Scope:               request.Scope,
			Identity:            material.Identity,
			Token:               material.Token,
			AuthURL:             material.AuthURL,
			Principal:           request.Principal,
			InsecureDevelopment: insecureDevelopment,
		}
		if provider.environment != "development" && provider.environment != "test" {
			if err := validateProductionCredential(credential); err != nil {
				return Credential{}, err
			}
		}
		return credential, nil
	}
	if provider.environment == "development" && provider.allowDevelopment && provider.developmentIdentity != "" {
		return Credential{
			Partition:           partition,
			Scope:               request.Scope,
			Identity:            provider.developmentIdentity,
			Principal:           request.Principal,
			InsecureDevelopment: true,
		}, nil
	}
	return Credential{}, fmt.Errorf("%w for partition %q and scope %q", ErrCredentialUnavailable, partition,
		request.Scope)
}

func ParseCredentialMap(value string) (map[string]CredentialMaterial, error) {
	if strings.TrimSpace(value) == "" {
		return map[string]CredentialMaterial{}, nil
	}
	var result map[string]CredentialMaterial
	if err := json.Unmarshal([]byte(value), &result); err != nil || result == nil {
		return nil, errors.New("LOREHUB_LORE_CREDENTIALS must be a JSON object keyed by repository partition")
	}
	for partition, material := range result {
		if strings.TrimSpace(partition) == "" {
			return nil, errors.New("LOREHUB_LORE_CREDENTIALS contains an empty partition")
		}
		result[partition] = CredentialMaterial{
			Identity: strings.TrimSpace(material.Identity),
			Token:    strings.TrimSpace(material.Token),
			AuthURL:  strings.TrimSpace(material.AuthURL),
		}
	}
	return result, nil
}

func ValidateCredential(repository RepositoryRef, credential Credential, scope Scope) error {
	if scope != ScopeRead && scope != ScopeWrite {
		return errors.New("unsupported Lore credential scope")
	}
	if credential.Scope != scope {
		return fmt.Errorf("Lore credential scope %q does not permit %q", credential.Scope, scope)
	}
	if !credential.Principal.valid() {
		return ErrInvalidPrincipal
	}
	partition := repository.CanonicalPartition()
	if partition != "" && credential.Partition != partition {
		return errors.New("Lore credential partition does not match repository")
	}
	if credential.Partition == "" && partition != "" {
		return errors.New("Lore credential partition is required")
	}
	if credential.InsecureDevelopment {
		if credential.Token != "" || credential.AuthURL != "" {
			return errors.New("insecure development credential cannot contain production auth material")
		}
		if credential.Identity == "" {
			return ErrCredentialUnavailable
		}
		return nil
	}
	return validateProductionCredential(credential)
}

func validateCredentialRequest(request CredentialRequest) error {
	if !request.Principal.valid() {
		return ErrInvalidPrincipal
	}
	if request.Scope != ScopeRead && request.Scope != ScopeWrite {
		return errors.New("unsupported Lore credential scope")
	}
	partition := request.Repository.CanonicalPartition()
	if partition == "" && request.Principal.ServicePurpose != ServicePurposeRepositoryRegistration {
		return errors.New("Lore repository partition is required")
	}
	return nil
}

func validateProductionMaterial(material CredentialMaterial) error {
	if material.Identity == "" || material.Token == "" || material.AuthURL == "" {
		return errors.New("production Lore credentials require identity, token, and auth URL")
	}
	if err := validateAuthURL(material.AuthURL); err != nil {
		return err
	}
	return nil
}

func validateProductionCredential(credential Credential) error {
	if credential.Identity == "" || credential.Token == "" || credential.AuthURL == "" {
		return ErrCredentialUnavailable
	}
	return validateAuthURL(credential.AuthURL)
}

func validateAuthURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("Lore AuthURL must be an absolute URL without credentials or fragments")
	}
	return nil
}
