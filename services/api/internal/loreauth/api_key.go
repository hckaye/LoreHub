package loreauth

import (
	"context"
	"errors"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/authz"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type APIKeyAuthenticator interface {
	AuthenticateAPIKey(context.Context, string) (auth.Principal, error)
	ValidateAPIKeyCredential(context.Context, string, string) error
}

type ServiceOption func(*Service) error

func WithAPIKeyAuthenticator(authenticator APIKeyAuthenticator) ServiceOption {
	return func(service *Service) error {
		if authenticator == nil {
			return errors.New("Lore API key authenticator is required")
		}
		service.apiKeys = authenticator
		return nil
	}
}

func (service *Service) ExchangeAPIKeyForUserToken(
	ctx context.Context,
	request *ExchangeAPIKeyForUserTokenRequest,
) (*ExchangeAPIKeyForUserTokenResponse, error) {
	if request == nil || strings.TrimSpace(request.GetApiKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "API key is required")
	}
	if service.apiKeys == nil {
		return nil, status.Error(codes.Unavailable, "API key exchange is unavailable")
	}
	principal, err := service.apiKeys.AuthenticateAPIKey(ctx, strings.TrimSpace(request.GetApiKey()))
	if errors.Is(err, auth.ErrAuthenticationUnavailable) {
		return nil, status.Error(codes.Unavailable, "API key validation is unavailable")
	}
	if err != nil || principal.CredentialKind != auth.CredentialPersonalAccessToken ||
		principal.InternalUserID == "" || principal.CredentialID == "" {
		return nil, status.Error(codes.Unauthenticated, "API key is invalid")
	}
	if !auth.PersonalAccessTokenAllowsRepository(principal.Scopes) {
		return nil, status.Error(codes.PermissionDenied, "API key does not allow Lore repository access")
	}
	user, err := service.policy.UserInfo(ctx, principal.InternalUserID)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "authenticated user is unavailable")
	}
	raw, expiresAt, err := service.tokens.MintAuthenticationTokenWithScopes(
		user,
		principal.Scopes,
		principal.CredentialID,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, "could not issue Lore authentication token")
	}
	return &ExchangeAPIKeyForUserTokenResponse{UserToken: &UserToken{
		UserToken: raw,
		ExpiresAt: expiresAt.UnixMilli(),
		UserId:    user.ID,
		UserName:  user.DisplayName,
	}}, nil
}

func (service *Service) ExchangeExternalTokenForUserToken(
	ctx context.Context,
	request *ExchangeExternalTokenForUserTokenRequest,
) (*ExchangeExternalTokenForUserTokenResponse, error) {
	if request == nil || request.GetTokenType() != loreclient.AuthenticationTokenType ||
		strings.TrimSpace(request.GetExternalToken()) == "" {
		return nil, status.Error(codes.InvalidArgument, "external token type is not supported")
	}
	verified, err := service.tokens.VerifyAuthenticationToken(strings.TrimSpace(request.GetExternalToken()))
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "external token is invalid")
	}
	claims := verified.Claims
	if err := service.validateAPIKeyCredential(ctx, claims); err != nil {
		return nil, err
	}
	return &ExchangeExternalTokenForUserTokenResponse{UserToken: &UserToken{
		UserToken: request.GetExternalToken(),
		ExpiresAt: claims.Expiry.Time().UnixMilli(),
		UserId:    claims.Subject,
		UserName:  claims.Name,
	}}, nil
}

func (service *Service) validateAPIKeyCredential(ctx context.Context, claims LoreClaims) error {
	if claims.CredentialID == "" {
		return nil
	}
	if service.apiKeys == nil {
		return status.Error(codes.Unavailable, "API key validation is unavailable")
	}
	if err := service.apiKeys.ValidateAPIKeyCredential(ctx, claims.CredentialID, claims.Subject); err != nil {
		if errors.Is(err, auth.ErrAuthenticationUnavailable) {
			return status.Error(codes.Unavailable, "API key validation is unavailable")
		}
		return status.Error(codes.PermissionDenied, "API key is no longer active")
	}
	return nil
}

func permissionsForAuthenticationToken(claims LoreClaims, permissions []string) ([]string, bool) {
	available := authz.ExpandPermissions(permissionMap(permissions))
	if len(claims.TokenScopes) == 0 {
		if !available[authz.PermissionRead] {
			return nil, false
		}
		return authz.PermissionList(available), true
	}
	scopes := make(map[string]bool, len(claims.TokenScopes))
	for _, scope := range claims.TokenScopes {
		scopes[scope] = true
	}
	narrowed := map[string]bool{}
	if scopes[auth.ScopeReadRepository] || scopes[auth.ScopeWriteRepository] {
		narrowed[authz.PermissionRead] = available[authz.PermissionRead]
	}
	if scopes[auth.ScopeWriteRepository] {
		narrowed[authz.PermissionWrite] = available[authz.PermissionWrite]
	}
	if !narrowed[authz.PermissionRead] {
		return nil, false
	}
	return authz.PermissionList(narrowed), true
}
