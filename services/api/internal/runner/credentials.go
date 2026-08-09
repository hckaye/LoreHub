package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const ReadLoreScope = "read"

type CredentialPrincipal struct {
	Kind    string
	Subject string
}

type CredentialRequest struct {
	Principal    CredentialPrincipal
	RepositoryID string
	LoreURL      string
	Scope        string
}

type LoreCredential struct {
	RepositoryID string
	Scope        string
	Token        string
	AuthURL      string
	Identity     string
	ExpiresAt    time.Time
}

type CredentialIssuer interface {
	Issue(ctx context.Context, request CredentialRequest) (LoreCredential, error)
}

type developmentCredentialIssuer struct {
	identity string
}

// NewDevelopmentCredentialIssuer is intentionally an in-memory local/test adapter.
func NewDevelopmentCredentialIssuer(identity string) CredentialIssuer {
	return developmentCredentialIssuer{identity: identity}
}

func (issuer developmentCredentialIssuer) Issue(
	ctx context.Context,
	request CredentialRequest,
) (LoreCredential, error) {
	if err := validateCredentialRequest(ctx, request); err != nil {
		return LoreCredential{}, err
	}
	if strings.TrimSpace(issuer.identity) == "" {
		return LoreCredential{}, errors.New("development Lore identity is empty")
	}
	return LoreCredential{
		RepositoryID: request.RepositoryID,
		Scope:        request.Scope,
		Identity:     issuer.identity,
		ExpiresAt:    time.Now().UTC().Add(5 * time.Minute),
	}, nil
}

type failClosedCredentialIssuer struct{}

func NewFailClosedCredentialIssuer() CredentialIssuer {
	return failClosedCredentialIssuer{}
}

func (failClosedCredentialIssuer) Issue(context.Context, CredentialRequest) (LoreCredential, error) {
	return LoreCredential{}, errors.New("repository-scoped Lore credential issuer is not configured")
}

func issueLoreCredential(
	ctx context.Context,
	issuer CredentialIssuer,
	principal CredentialPrincipal,
	repositoryID string,
	loreURL string,
) (LoreCredential, error) {
	if issuer == nil {
		return LoreCredential{}, errors.New("repository-scoped Lore credential issuer is not configured")
	}
	request := CredentialRequest{
		Principal:    principal,
		RepositoryID: repositoryID,
		LoreURL:      loreURL,
		Scope:        ReadLoreScope,
	}
	if err := validateCredentialRequest(ctx, request); err != nil {
		return LoreCredential{}, err
	}
	credential, err := issuer.Issue(ctx, request)
	if err != nil {
		return LoreCredential{}, fmt.Errorf("issue repository Lore credential: %w", err)
	}
	if credential.RepositoryID != request.RepositoryID || credential.Scope != request.Scope {
		return LoreCredential{}, errors.New("Lore credential does not match the requested resource or scope")
	}
	now := time.Now().UTC()
	if credential.ExpiresAt.IsZero() || !credential.ExpiresAt.After(now) {
		return LoreCredential{}, errors.New("Lore credential is expired or has no expiry")
	}
	if credential.ExpiresAt.After(now.Add(15 * time.Minute)) {
		return LoreCredential{}, errors.New("Lore credential lifetime exceeds the short-lived limit")
	}
	if strings.TrimSpace(credential.Token) == "" && strings.TrimSpace(credential.AuthURL) == "" &&
		strings.TrimSpace(credential.Identity) == "" {
		return LoreCredential{}, errors.New("Lore credential contains no usable authentication material")
	}
	return credential, nil
}

func issueLoreIdentity(
	ctx context.Context,
	issuer CredentialIssuer,
	principal CredentialPrincipal,
	repositoryID string,
	loreURL string,
) (string, error) {
	credential, err := issueLoreCredential(ctx, issuer, principal, repositoryID, loreURL)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(credential.Identity) == "" {
		return "", errors.New("the configured Lore client requires an identity credential")
	}
	return credential.Identity, nil
}

func validateCredentialRequest(ctx context.Context, request CredentialRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if request.Principal.Kind == "" || request.Principal.Subject == "" {
		return errors.New("credential principal kind and subject are required")
	}
	if request.RepositoryID == "" || request.LoreURL == "" {
		return errors.New("repository partition and Lore URL are required for a credential request")
	}
	if request.Scope != ReadLoreScope {
		return fmt.Errorf("Lore credential scope %q is not supported", request.Scope)
	}
	return nil
}
