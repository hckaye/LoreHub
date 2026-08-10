package loreauth

import (
	"context"
	"errors"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

// CredentialIssuer is the narrow adapter between the Lore auth service and
// SDK callers. It issues a new credential for every complete request.
type CredentialIssuer struct {
	service *Service
}

func NewCredentialIssuer(service *Service) (*CredentialIssuer, error) {
	if service == nil {
		return nil, errors.New("Lore credential issuer requires the auth service")
	}
	return &CredentialIssuer{service: service}, nil
}

func (issuer *CredentialIssuer) IssueCredential(
	ctx context.Context,
	request loreclient.CredentialRequest,
) (loreclient.Credential, error) {
	if issuer == nil || issuer.service == nil {
		return loreclient.Credential{}, errors.New("Lore credential issuer is unavailable")
	}
	partition, err := request.Repository.ValidatedPartition()
	if err != nil || request.Partition != partition {
		return loreclient.Credential{}, loreclient.ErrCredentialContract
	}
	if request.Scope != loreclient.ScopeRead && request.Scope != loreclient.ScopeWrite {
		return loreclient.Credential{}, loreclient.ErrCredentialContract
	}
	requested := []string{"read"}
	if request.Scope == loreclient.ScopeWrite {
		requested = []string{"write"}
	}
	var credential loreclient.Credential
	if request.Principal.UserID != "" {
		credential, err = issuer.service.IssueResourceToken(
			ctx, request.Principal.UserID, "urc-"+request.Partition, requested,
		)
	} else {
		credential, err = issuer.service.IssueServiceResourceToken(ctx, request.Principal,
			"urc-"+request.Partition, requested)
	}
	if err != nil {
		return loreclient.Credential{}, err
	}
	return credential, nil
}

func servicePrincipalName(purpose string) (string, error) {
	switch purpose {
	case loreclient.ServicePurposePublicReader:
		return "lorehub-anonymous-reader", nil
	case loreclient.ServicePurposeActionsRunner:
		return "lorehub-ci-runner", nil
	case loreclient.ServicePurposeObserver:
		return "lorehub-observer", nil
	case loreclient.ServicePurposeRepositoryRegistration:
		return "lorehub-provisioner", nil
	default:
		return "", errors.New("unknown Lore service principal purpose")
	}
}
