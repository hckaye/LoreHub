package lore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Scope string

const (
	ScopeRead  Scope = "repository:read"
	ScopeWrite Scope = "repository:write"
)

var ErrCredentialUnavailable = errors.New("Lore repository credential is unavailable")

type Credential struct {
	Partition string
	Identity  string
	Scope     Scope
}

type CredentialProvider interface {
	ForRepository(context.Context, RepositoryRef, Scope) (Credential, error)
}

type configuredCredentialProvider struct {
	environment         string
	identities          map[string]string
	developmentIdentity string
	allowDevelopment    bool
}

func NewCredentialProvider(
	environment string,
	identities map[string]string,
	developmentIdentity string,
	allowDevelopmentFallback bool,
) (CredentialProvider, error) {
	if environment == "" {
		environment = "production"
	}
	clean := make(map[string]string, len(identities))
	for partition, identity := range identities {
		partition = strings.TrimSpace(partition)
		identity = strings.TrimSpace(identity)
		if partition == "" || identity == "" {
			return nil, errors.New("Lore credentials require non-empty partitions and identities")
		}
		clean[partition] = identity
	}
	if environment != "development" && allowDevelopmentFallback {
		return nil, errors.New("development Lore credential fallback is not allowed outside development")
	}
	return configuredCredentialProvider{
		environment:         environment,
		identities:          clean,
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
	repository RepositoryRef,
	scope Scope,
) (Credential, error) {
	if err := ctx.Err(); err != nil {
		return Credential{}, err
	}
	if scope != ScopeRead && scope != ScopeWrite {
		return Credential{}, errors.New("unsupported Lore credential scope")
	}
	partition := strings.TrimSpace(repository.LoreRepositoryID)
	if identity, ok := provider.identities[partition]; ok {
		return Credential{Partition: partition, Identity: identity, Scope: scope}, nil
	}
	if provider.environment == "development" && provider.allowDevelopment && provider.developmentIdentity != "" {
		return Credential{Partition: partition, Identity: provider.developmentIdentity, Scope: scope}, nil
	}
	return Credential{}, fmt.Errorf("%w for partition %q and scope %q", ErrCredentialUnavailable, partition, scope)
}

func ParseCredentialMap(value string) (map[string]string, error) {
	if strings.TrimSpace(value) == "" {
		return map[string]string{}, nil
	}
	var identities map[string]string
	if err := json.Unmarshal([]byte(value), &identities); err != nil {
		return nil, fmt.Errorf("parse Lore credential map: %w", err)
	}
	if identities == nil {
		return map[string]string{}, nil
	}
	return identities, nil
}

func ValidateCredential(repository RepositoryRef, credential Credential, scope Scope) error {
	if credential.Scope != scope {
		return fmt.Errorf("Lore credential scope %q does not permit %q", credential.Scope, scope)
	}
	if credential.Identity == "" {
		return ErrCredentialUnavailable
	}
	if repository.LoreRepositoryID != "" && credential.Partition != repository.LoreRepositoryID {
		return errors.New("Lore credential partition does not match repository")
	}
	return nil
}
