package loreauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/authz"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Service struct {
	UnimplementedUrcAuthApiServer
	policy     authz.Store
	sessions   authz.SessionStore
	tokens     *TokenService
	loginURL   string
	authURL    string
	sessionTTL time.Duration
	apiKeys    APIKeyAuthenticator
}

func NewService(
	policy authz.Store,
	sessions authz.SessionStore,
	tokens *TokenService,
	loginURL string,
	authURL string,
	sessionTTL time.Duration,
	allowInsecureLoginURL bool,
	options ...ServiceOption,
) (*Service, error) {
	if policy == nil || sessions == nil || tokens == nil {
		return nil, errors.New("Lore auth service dependencies are required")
	}
	if sessionTTL <= 0 || sessionTTL > 10*time.Minute {
		return nil, errors.New("Lore auth session TTL must be between one second and ten minutes")
	}
	parsed, err := url.Parse(loginURL)
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Path != "/auth/lore/confirm" ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("Lore auth login URL must be a fixed HTTPS confirmation endpoint")
	}
	if parsed.Scheme != "https" && !(allowInsecureLoginURL && parsed.Scheme == "http") {
		return nil, errors.New("Lore auth login URL must use HTTPS outside local development")
	}
	if err := loreclient.ValidateAuthURL(authURL); err != nil {
		return nil, errors.New("Lore auth URL must be a fixed ucs-auth endpoint")
	}
	service := &Service{policy: policy, sessions: sessions, tokens: tokens,
		loginURL:   strings.TrimRight(loginURL, "/"),
		authURL:    authURL,
		sessionTTL: sessionTTL}
	for _, option := range options {
		if option != nil {
			if err := option(service); err != nil {
				return nil, err
			}
		}
	}
	return service, nil
}

func (service *Service) ConfirmSession(ctx context.Context, sessionCode string, userID string) error {
	if len(sessionCode) < 32 || len(sessionCode) > 128 || userID == "" {
		return authz.ErrSessionNotFound
	}
	digest := sha256.Sum256([]byte(sessionCode))
	return service.sessions.ConfirmLoreAuthSession(ctx, digest[:], userID)
}

func (service *Service) JWKS() map[string]any {
	return service.tokens.JWKS()
}

func (service *Service) AuthURL() string {
	return service.authURL
}

func (service *Service) VerifyUserToken(raw string) (LoreClaims, error) {
	verified, err := service.tokens.VerifyResourceToken(raw)
	if err != nil {
		return LoreClaims{}, errors.New("invalid Lore user token")
	}
	return verified.Claims, nil
}

func (service *Service) AuthorizeUserToken(
	ctx context.Context,
	raw string,
	resourceID string,
	permission string,
) (LoreClaims, error) {
	verified, err := service.tokens.VerifyResourceToken(raw)
	if err != nil || verified.Claims.IsServiceAccount || !authz.ValidResourceID(resourceID) {
		return LoreClaims{}, errors.New("invalid Lore user token")
	}
	if !service.hasCurrentPermission(ctx, verified.Claims, resourceID, permission) {
		return LoreClaims{}, errors.New("Lore user token is not currently authorized")
	}
	return verified.Claims, nil
}

func (service *Service) IssueResourceToken(
	ctx context.Context,
	userID string,
	resourceID string,
	requested []string,
) (loreclient.Credential, error) {
	if !authz.ValidResourceID(resourceID) {
		return loreclient.Credential{}, authz.ErrInvalidResource
	}
	current, err := service.policy.EffectivePermissions(ctx, userID, resourceID)
	if err != nil {
		return loreclient.Credential{}, err
	}
	narrowed, err := authz.IntersectPermissions(permissionMap(current.Permissions), requested)
	if err != nil {
		return loreclient.Credential{}, err
	}
	if len(narrowed) == 0 {
		return loreclient.Credential{}, authz.ErrScopeWidened
	}
	user, err := service.policy.UserInfo(ctx, userID)
	if err != nil {
		return loreclient.Credential{}, err
	}
	authenticationToken, _, err := service.tokens.MintAuthenticationToken(user)
	if err != nil {
		return loreclient.Credential{}, err
	}
	token, _, err := service.tokens.MintResourceToken(user, []LoreResourcePermission{{
		ResourceID: resourceID,
		Permission: authz.PermissionList(narrowed),
	}})
	if err != nil {
		return loreclient.Credential{}, err
	}
	return service.credentialFromToken(token, authenticationToken, resourceID, requested,
		loreclient.UserPrincipal(userID))
}

func (service *Service) IssueAuthenticationToken(
	ctx context.Context,
	userID string,
) (string, time.Time, error) {
	user, err := service.policy.UserInfo(ctx, userID)
	if err != nil {
		return "", time.Time{}, err
	}
	return service.tokens.MintAuthenticationToken(user)
}

type servicePrincipalPolicy interface {
	ServicePrincipalResource(
		ctx context.Context,
		name string,
		resourceID string,
	) (authz.UserInfo, []string, error)
}

func (service *Service) IssueServiceResourceToken(
	ctx context.Context,
	principal loreclient.Principal,
	resourceID string,
	requested []string,
) (loreclient.Credential, error) {
	if !authz.ValidResourceID(resourceID) {
		return loreclient.Credential{}, authz.ErrInvalidResource
	}
	if principal.UserID != "" || principal.ServicePurpose == "" || principal.Subject == "" {
		return loreclient.Credential{}, loreclient.ErrInvalidPrincipal
	}
	principalName, err := servicePrincipalName(principal.ServicePurpose)
	if err != nil {
		return loreclient.Credential{}, err
	}
	policy, ok := service.policy.(servicePrincipalPolicy)
	if !ok {
		return loreclient.Credential{}, errors.New("service principal policy is unavailable")
	}
	servicePrincipal, granted, err := policy.ServicePrincipalResource(ctx, principalName, resourceID)
	if err != nil {
		return loreclient.Credential{}, err
	}
	if servicePrincipal.ID != principal.Subject {
		return loreclient.Credential{}, loreclient.ErrCredentialContract
	}
	narrowed, err := authz.IntersectPermissions(permissionMap(granted), requested)
	if err != nil || len(narrowed) == 0 {
		if err != nil {
			return loreclient.Credential{}, err
		}
		return loreclient.Credential{}, authz.ErrScopeWidened
	}
	token, _, err := service.tokens.MintServiceResourceToken(servicePrincipal, []LoreResourcePermission{{
		ResourceID: resourceID,
		Permission: authz.PermissionList(narrowed),
	}})
	if err != nil {
		return loreclient.Credential{}, err
	}
	authenticationToken, _, err := service.tokens.MintServiceAuthenticationToken(servicePrincipal)
	if err != nil {
		return loreclient.Credential{}, err
	}
	return service.credentialFromToken(token, authenticationToken, resourceID, requested, principal)
}

func (service *Service) credentialFromToken(
	rawToken string,
	authenticationToken string,
	resourceID string,
	requested []string,
	principal loreclient.Principal,
) (loreclient.Credential, error) {
	verified, err := service.tokens.VerifyResourceToken(rawToken)
	if err != nil {
		return loreclient.Credential{}, errors.New("issued Lore token could not be verified")
	}
	principalSubject := principal.UserID
	if principalSubject == "" {
		principalSubject = principal.Subject
	}
	if verified.Claims.Subject != principalSubject ||
		verified.Claims.IsServiceAccount != (principal.ServicePurpose != "") {
		return loreclient.Credential{}, loreclient.ErrCredentialContract
	}
	verifiedAuthentication, err := service.tokens.VerifyAuthenticationToken(authenticationToken)
	if err != nil || verifiedAuthentication.Claims.Subject != principalSubject ||
		verifiedAuthentication.Claims.IsServiceAccount != (principal.ServicePurpose != "") ||
		len(verifiedAuthentication.Claims.Resources) != 0 {
		return loreclient.Credential{}, loreclient.ErrCredentialContract
	}
	permissions, found := resourcePermissions(verified.Claims.Resources, resourceID)
	if !found || len(permissions) == 0 {
		return loreclient.Credential{}, errors.New("issued Lore token has no exact resource scope")
	}
	scope := loreclient.ScopeRead
	if containsPermission(permissions, authz.PermissionObliterate) {
		scope = loreclient.ScopeObliterate
	} else if containsPermission(permissions, authz.PermissionAdmin) {
		scope = loreclient.ScopeAdmin
	} else if containsPermission(permissions, authz.PermissionWrite) {
		scope = loreclient.ScopeWrite
	}
	return loreclient.Credential{
		Partition:               strings.TrimPrefix(resourceID, "urc-"),
		Scope:                   scope,
		ResourceID:              resourceID,
		Subject:                 verified.Claims.Subject,
		RequestedScopes:         []string{string(scopeForRequested(requested))},
		GrantedScopes:           []string{string(scope)},
		Identity:                verified.Claims.Subject,
		Token:                   rawToken,
		AuthenticationToken:     authenticationToken,
		AuthURL:                 service.authURL,
		ExpiresAt:               verified.Claims.Expiry.Time(),
		AuthenticationExpiresAt: verifiedAuthentication.Claims.Expiry.Time(),
		Principal:               principal,
	}, nil
}

func scopeForRequested(requested []string) loreclient.Scope {
	for _, permission := range requested {
		if permission == authz.PermissionObliterate {
			return loreclient.ScopeObliterate
		}
		if permission == authz.PermissionAdmin {
			return loreclient.ScopeAdmin
		}
		if permission == authz.PermissionWrite {
			return loreclient.ScopeWrite
		}
	}
	return loreclient.ScopeRead
}

func containsPermission(permissions []string, wanted string) bool {
	for _, permission := range permissions {
		if permission == wanted {
			return true
		}
	}
	return false
}

func (service *Service) HealthCheck(
	context.Context,
	*HealthCheckRequest,
) (*HealthCheckResponse, error) {
	return &HealthCheckResponse{Status: "ok"}, nil
}

func (service *Service) StartAuthSession(
	ctx context.Context,
	request *StartAuthSessionRequest,
) (*StartAuthSessionResponse, error) {
	if request == nil || len(request.GetClientState()) < 16 || len(request.GetClientState()) > 512 {
		return nil, status.Error(codes.InvalidArgument, "client_state is invalid")
	}
	codeBytes := make([]byte, 32)
	if _, err := rand.Read(codeBytes); err != nil {
		return nil, status.Error(codes.Internal, "could not create authentication session")
	}
	code := base64Raw(codeBytes)
	codeDigest := sha256.Sum256([]byte(code))
	stateDigest := sha256.Sum256([]byte(request.GetClientState()))
	if err := service.sessions.CreateLoreAuthSession(ctx, uuid.NewString(), codeDigest[:], stateDigest[:],
		time.Now().UTC().Add(service.sessionTTL)); err != nil {
		return nil, status.Error(codes.Internal, "could not create authentication session")
	}
	return &StartAuthSessionResponse{
		SessionCode: code,
		LoginUrl:    service.loginURL + "?session=" + url.QueryEscape(code),
	}, nil
}

func (service *Service) GetAuthSession(
	ctx context.Context,
	request *GetAuthSessionRequest,
) (*GetAuthSessionResponse, error) {
	if request == nil || len(request.GetSessionCode()) < 32 || len(request.GetClientState()) < 16 {
		return nil, status.Error(codes.InvalidArgument, "authentication session request is invalid")
	}
	codeDigest := sha256.Sum256([]byte(request.GetSessionCode()))
	stateDigest := sha256.Sum256([]byte(request.GetClientState()))
	poll, err := service.sessions.PollLoreAuthSession(ctx, codeDigest[:], stateDigest[:])
	if err != nil {
		switch {
		case errors.Is(err, authz.ErrSessionRateLimited):
			return nil, status.Error(codes.ResourceExhausted, "authentication polling is too frequent")
		case errors.Is(err, authz.ErrSessionState):
			return nil, status.Error(codes.PermissionDenied, "authentication session is invalid")
		case errors.Is(err, authz.ErrSessionNotFound), errors.Is(err, authz.ErrSessionConsumed):
			return nil, status.Error(codes.NotFound, "authentication session is unavailable")
		default:
			return nil, status.Error(codes.Internal, "could not read authentication session")
		}
	}
	if !poll.Ready || poll.UserID == "" {
		return &GetAuthSessionResponse{}, nil
	}
	user, err := service.policy.UserInfo(ctx, poll.UserID)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "authenticated user is unavailable")
	}
	rawToken, expiresAt, err := service.tokens.MintAuthenticationToken(user)
	if err != nil {
		return nil, status.Error(codes.Internal, "could not issue user token")
	}
	return &GetAuthSessionResponse{UserToken: &UserToken{
		UserToken: rawToken,
		ExpiresAt: expiresAt.UnixMilli(),
		UserId:    user.ID,
		UserName:  user.DisplayName,
	}}, nil
}

func (service *Service) RefreshAuthSession(
	context.Context,
	*RefreshAuthSessionRequest,
) (*RefreshAuthSessionResponse, error) {
	return nil, status.Error(codes.Unimplemented, "refresh authentication sessions are not supported")
}

func (service *Service) VerifyUser(
	ctx context.Context,
	request *VerifyUserRequest,
) (*VerifyUserResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "verify user request is required")
	}
	claims, err := service.authenticateResource(ctx)
	if err != nil {
		return nil, err
	}
	if !service.hasCurrentReadPermission(ctx, claims) {
		return nil, status.Error(codes.PermissionDenied, "current Lore authorization is required")
	}
	userID, err := service.targetUserID(request.GetTargetUser(), claims)
	if err != nil {
		return nil, err
	}
	user, err := service.policy.UserInfo(ctx, userID)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "user is not available")
	}
	return &VerifyUserResponse{UserInfo: &UserInfo{UserId: user.ID, DisplayName: user.DisplayName}}, nil
}

func (service *Service) ExchangeUserTokenForMultiresourceToken(
	ctx context.Context,
	request *ExchangeUserTokenForMultiresourceTokenRequest,
) (*ExchangeUserTokenForMultiresourceTokenResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "resource exchange request is required")
	}
	claims, err := service.authenticateBase(ctx)
	if err != nil {
		return nil, err
	}
	if err := service.validateAPIKeyCredential(ctx, claims); err != nil {
		return nil, err
	}
	requested := request.GetResourceId()
	if len(requested) == 0 || len(requested) > 100 {
		return nil, status.Error(codes.InvalidArgument, "at least one Lore resource is required")
	}
	seen := make(map[string]bool, len(requested))
	resources := make([]LoreResourcePermission, 0, len(requested))
	for _, resourceID := range requested {
		resourceID = strings.TrimSpace(resourceID)
		if !authz.ValidResourceID(resourceID) || seen[resourceID] {
			return nil, status.Error(codes.InvalidArgument, "resource IDs must be exact urc-{repository_id} values")
		}
		seen[resourceID] = true
		var permissions []string
		if claims.IsServiceAccount {
			servicePolicy, ok := service.policy.(servicePrincipalPolicy)
			if !ok || claims.PreferredUsername == "" {
				return nil, status.Error(codes.PermissionDenied,
					"service principal authorization is unavailable")
			}
			principal, granted, policyErr := servicePolicy.ServicePrincipalResource(
				ctx, claims.PreferredUsername, resourceID,
			)
			if policyErr != nil || principal.ID != claims.Subject {
				return nil, status.Error(codes.PermissionDenied,
					"requested Lore resource is not authorized")
			}
			permissions = granted
		} else {
			current, permissionErr := service.policy.EffectivePermissions(ctx, claims.Subject, resourceID)
			if permissionErr != nil {
				return nil, status.Error(codes.PermissionDenied,
					"requested Lore resource is not authorized")
			}
			permissions = current.Permissions
		}
		if len(permissions) == 0 {
			return nil, status.Error(codes.PermissionDenied, "requested Lore resource is not authorized")
		}
		permissions, ok := permissionsForAuthenticationToken(claims, permissions)
		if !ok {
			return nil, status.Error(codes.PermissionDenied, "requested Lore resource is not authorized")
		}
		resources = append(resources, LoreResourcePermission{ResourceID: resourceID, Permission: permissions})
	}
	var user authz.UserInfo
	if claims.IsServiceAccount {
		servicePolicy := service.policy.(servicePrincipalPolicy)
		principal, _, serviceErr := servicePolicy.ServicePrincipalResource(ctx,
			claims.PreferredUsername, resources[0].ResourceID)
		if serviceErr != nil || principal.ID != claims.Subject {
			return nil, status.Error(codes.PermissionDenied, "service principal is unavailable")
		}
		user = principal
	} else {
		user, err = service.policy.UserInfo(ctx, claims.Subject)
		if err != nil {
			return nil, status.Error(codes.PermissionDenied, "authenticated user is unavailable")
		}
	}
	var rawToken string
	var expiresAt time.Time
	if claims.IsServiceAccount {
		rawToken, expiresAt, err = service.tokens.MintServiceResourceToken(user, resources)
	} else {
		rawToken, expiresAt, err = service.tokens.MintResourceToken(user, resources)
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "could not issue resource token")
	}
	return &ExchangeUserTokenForMultiresourceTokenResponse{Token: &UserToken{
		UserToken: rawToken,
		ExpiresAt: expiresAt.UnixMilli(),
		UserId:    user.ID,
		UserName:  user.DisplayName,
	}}, nil
}

func (service *Service) CheckUserPermission(
	ctx context.Context,
	request *CheckUserPermissionRequest,
) (*CheckUserPermissionResponse, error) {
	claims, resourceScoped, err := service.authenticatePermissionCaller(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil || len(request.GetResourceId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "resource IDs are required")
	}
	targetID, err := service.targetUserID(request.GetTargetUser(), claims)
	if err != nil {
		return nil, err
	}
	if !resourceScoped {
		return service.checkAuthenticationPermissions(ctx, claims, request.GetResourceId(), targetID), nil
	}
	response := &CheckUserPermissionResponse{}
	for _, resourceID := range request.GetResourceId() {
		if !authz.ValidResourceID(resourceID) {
			return nil, status.Error(codes.InvalidArgument, "resource IDs must be exact urc-{repository_id} values")
		}
		if !service.hasCurrentPermission(ctx, claims, resourceID, authz.PermissionRead) {
			return nil, status.Error(codes.PermissionDenied, "current resource access is not authorized")
		}
		permissions, err := service.policy.EffectivePermissions(ctx, targetID, resourceID)
		if err != nil {
			response.DeniedResourcePermission = append(response.DeniedResourcePermission,
				&ResourcePermission{ResourceId: resourceID})
			continue
		}
		response.AllowedResourcePermission = append(response.AllowedResourcePermission,
			&ResourcePermission{ResourceId: resourceID, Permission: permissions.Permissions})
	}
	return response, nil
}

func (service *Service) checkAuthenticationPermissions(
	ctx context.Context,
	claims LoreClaims,
	resourceIDs []string,
	targetID string,
) *CheckUserPermissionResponse {
	response := &CheckUserPermissionResponse{}
	for _, resourceID := range resourceIDs {
		denied := &ResourcePermission{ResourceId: resourceID}
		if !authz.ValidResourceID(resourceID) || targetID != claims.Subject {
			response.DeniedResourcePermission = append(response.DeniedResourcePermission, denied)
			continue
		}
		permissions, ok := service.authenticationPermissions(ctx, claims, resourceID)
		if !ok {
			response.DeniedResourcePermission = append(response.DeniedResourcePermission, denied)
			continue
		}
		response.AllowedResourcePermission = append(response.AllowedResourcePermission,
			&ResourcePermission{ResourceId: resourceID, Permission: permissions})
	}
	return response
}

func (service *Service) LookupUserPermissions(
	ctx context.Context,
	request *LookupUserPermissionsRequest,
) (*LookupUserPermissionsResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "permission lookup request is required")
	}
	claims, resourceScoped, err := service.authenticatePermissionCaller(ctx)
	if err != nil {
		return nil, err
	}
	pageSize := int(request.GetPageSize())
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	filter := strings.TrimSpace(request.GetResourceFilter())
	if filter != "" && filter != "urc" && filter != "urc-*" && !authz.ValidResourceID(filter) {
		return nil, status.Error(codes.InvalidArgument, "permission lookup filter is invalid")
	}
	if resourceScoped && authz.ValidResourceID(filter) &&
		!service.hasCurrentPermission(ctx, claims, filter, authz.PermissionRead) {
		return nil, status.Error(codes.PermissionDenied, "current resource access is not authorized")
	}
	var resources []authz.ResourcePermissions
	var next string
	if resourceScoped {
		resources, next, err = service.lookupScopedPermissions(
			ctx, claims, filter, pageSize, request.GetPageToken(),
		)
	} else {
		resources, next, err = service.lookupAuthenticationPermissions(
			ctx, claims, filter, pageSize, request.GetPageToken(),
		)
	}
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "permission lookup filter is invalid")
	}
	response := &LookupUserPermissionsResponse{}
	for _, resource := range resources {
		response.ResourcePermission = append(response.ResourcePermission, &ResourcePermission{
			ResourceId: resource.ResourceID,
			Permission: resource.Permissions,
		})
	}
	if next != "" {
		response.NextPageToken = &next
	}
	return response, nil
}

func (service *Service) GetUserInfo(
	ctx context.Context,
	request *GetUserInfoRequest,
) (*GetUserInfoResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "user info request is required")
	}
	claims, err := service.authenticateResource(ctx)
	if err != nil {
		return nil, err
	}
	if !authz.ValidResourceID(request.GetResourceId()) ||
		!service.hasCurrentPermission(ctx, claims, request.GetResourceId(), authz.PermissionRead) {
		return nil, status.Error(codes.PermissionDenied, "resource access is not authorized")
	}
	users, err := service.policy.UserInfoForResource(ctx, request.GetResourceId(), request.GetUserId())
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, "resource users are unavailable")
	}
	response := &GetUserInfoResponse{}
	for _, user := range users {
		response.UserInfo = append(response.UserInfo, &UserInfo{UserId: user.ID, DisplayName: user.DisplayName})
	}
	return response, nil
}

func (service *Service) GetUserId(
	ctx context.Context,
	request *GetUserIdRequest,
) (*GetUserIdResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "user ID request is required")
	}
	claims, err := service.authenticateResource(ctx)
	if err != nil {
		return nil, err
	}
	if !authz.ValidResourceID(request.GetResourceId()) ||
		!service.hasCurrentPermission(ctx, claims, request.GetResourceId(), authz.PermissionRead) {
		return nil, status.Error(codes.PermissionDenied, "resource access is not authorized")
	}
	user, err := service.policy.UserInfoByDisplayName(ctx, request.GetResourceId(), request.GetUserDisplayName())
	if err != nil {
		return nil, status.Error(codes.NotFound, "user is not available")
	}
	return &GetUserIdResponse{UserInfo: &UserInfo{UserId: user.ID, DisplayName: user.DisplayName}}, nil
}

func (service *Service) GetProviderUserId(
	ctx context.Context,
	request *GetProviderUserIdRequest,
) (*GetProviderUserIdResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "provider user ID request is required")
	}
	claims, err := service.authenticateResource(ctx)
	if err != nil {
		return nil, err
	}
	if !service.hasCurrentReadPermission(ctx, claims) {
		return nil, status.Error(codes.PermissionDenied, "current Lore authorization is required")
	}
	if request.GetUserId() != claims.Subject {
		return nil, status.Error(codes.PermissionDenied, "provider identity lookup is limited to the current user")
	}
	providerSubject, err := service.policy.ProviderSubject(ctx, claims.Subject)
	if err != nil {
		return nil, status.Error(codes.NotFound, "provider identity is unavailable")
	}
	return &GetProviderUserIdResponse{UserId: claims.Subject, ProviderUserId: providerSubject}, nil
}

func (service *Service) authenticateBase(ctx context.Context) (LoreClaims, error) {
	return service.authenticateWith(ctx, service.tokens.VerifyAuthenticationToken)
}

func (service *Service) authenticateResource(ctx context.Context) (LoreClaims, error) {
	claims, err := service.authenticateWith(ctx, service.tokens.VerifyResourceToken)
	if err != nil {
		return LoreClaims{}, err
	}
	if claims.IsServiceAccount {
		return claims, nil
	}
	if _, err := service.policy.UserInfo(ctx, claims.Subject); err != nil {
		return LoreClaims{}, status.Error(codes.PermissionDenied, "the user is no longer active")
	}
	return claims, nil
}

func (service *Service) authenticatePermissionCaller(
	ctx context.Context,
) (LoreClaims, bool, error) {
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) == 0 {
		return LoreClaims{}, false, status.Error(codes.Unauthenticated, "authorization is required")
	}
	parts := strings.SplitN(strings.TrimSpace(values[0]), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") || strings.TrimSpace(parts[1]) == "" {
		return LoreClaims{}, false, status.Error(codes.Unauthenticated, "authorization is required")
	}
	raw := strings.TrimSpace(parts[1])
	if verified, err := service.tokens.VerifyResourceToken(raw); err == nil {
		if err := service.ensureActivePermissionCaller(ctx, verified.Claims); err != nil {
			return LoreClaims{}, false, err
		}
		return verified.Claims, true, nil
	}
	verified, err := service.tokens.VerifyAuthenticationToken(raw)
	if err != nil {
		return LoreClaims{}, false, status.Error(codes.Unauthenticated, "authorization is invalid")
	}
	if err := service.ensureActivePermissionCaller(ctx, verified.Claims); err != nil {
		return LoreClaims{}, false, err
	}
	return verified.Claims, false, nil
}

func (service *Service) ensureActivePermissionCaller(ctx context.Context, claims LoreClaims) error {
	if err := service.validateAPIKeyCredential(ctx, claims); err != nil {
		return err
	}
	if claims.IsServiceAccount {
		if claims.Subject == "" || claims.PreferredUsername == "" {
			return status.Error(codes.PermissionDenied, "service principal is unavailable")
		}
		return nil
	}
	if _, err := service.policy.UserInfo(ctx, claims.Subject); err != nil {
		return status.Error(codes.PermissionDenied, "the user is no longer active")
	}
	return nil
}

func (service *Service) authenticateWith(
	ctx context.Context,
	verify func(string) (VerifiedToken, error),
) (LoreClaims, error) {
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) == 0 {
		return LoreClaims{}, status.Error(codes.Unauthenticated, "authorization is required")
	}
	parts := strings.SplitN(strings.TrimSpace(values[0]), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") || strings.TrimSpace(parts[1]) == "" {
		return LoreClaims{}, status.Error(codes.Unauthenticated, "authorization is required")
	}
	verified, err := verify(strings.TrimSpace(parts[1]))
	if err != nil {
		return LoreClaims{}, status.Error(codes.Unauthenticated, "authorization is invalid")
	}
	return verified.Claims, nil
}

func (service *Service) targetUserID(target *TargetUser, caller LoreClaims) (string, error) {
	if target == nil || target.GetUserToken() == "" {
		return caller.Subject, nil
	}
	verified, err := service.tokens.VerifyResourceToken(target.GetUserToken())
	if err != nil {
		return "", status.Error(codes.PermissionDenied, "target user token is invalid")
	}
	if verified.Claims.Subject != caller.Subject {
		return "", status.Error(codes.PermissionDenied, "target user lookup is limited to the current user")
	}
	return caller.Subject, nil
}

func resourcePermissions(resources []LoreResourcePermission, resourceID string) ([]string, bool) {
	for _, resource := range resources {
		if resource.ResourceID == resourceID {
			return resource.Permission, true
		}
	}
	return nil, false
}

func permissionMap(permissions []string) map[string]bool {
	result := make(map[string]bool, len(permissions))
	for _, permission := range permissions {
		result[permission] = true
	}
	return result
}

func (service *Service) tokenAllowsResource(
	claims LoreClaims,
	resourceID string,
	permission string,
) bool {
	if len(claims.Resources) == 0 {
		return false
	}
	permissions, found := resourcePermissions(claims.Resources, resourceID)
	if !found {
		return false
	}
	available := authz.ExpandPermissions(permissionMap(permissions))
	if permission == authz.PermissionRead {
		return available[authz.PermissionRead] || available[authz.PermissionWrite] ||
			available[authz.PermissionAdmin]
	}
	return available[permission]
}

func (service *Service) hasCurrentPermission(
	ctx context.Context,
	claims LoreClaims,
	resourceID string,
	permission string,
) bool {
	if !service.tokenAllowsResource(claims, resourceID, permission) {
		return false
	}
	narrowed, ok := service.currentTokenPermissions(ctx, claims, resourceID)
	if !ok {
		return false
	}
	return narrowed[permission]
}

func (service *Service) hasCurrentReadPermission(ctx context.Context, claims LoreClaims) bool {
	for _, resource := range claims.Resources {
		if service.hasCurrentPermission(ctx, claims, resource.ResourceID, authz.PermissionRead) {
			return true
		}
	}
	return false
}

func (service *Service) currentTokenPermissions(
	ctx context.Context,
	claims LoreClaims,
	resourceID string,
) (map[string]bool, bool) {
	current, err := service.policy.EffectivePermissions(ctx, claims.Subject, resourceID)
	if err != nil {
		return nil, false
	}
	tokenPermissions, found := resourcePermissions(claims.Resources, resourceID)
	if !found {
		return nil, false
	}
	available := authz.ExpandPermissions(permissionMap(current.Permissions))
	narrowed := authz.ExpandPermissions(permissionMap(tokenPermissions))
	intersection := make(map[string]bool)
	for permission := range available {
		if narrowed[permission] {
			intersection[permission] = true
		}
	}
	return intersection, len(intersection) > 0
}

func (service *Service) lookupScopedPermissions(
	ctx context.Context,
	claims LoreClaims,
	filter string,
	pageSize int,
	pageToken string,
) ([]authz.ResourcePermissions, string, error) {
	offset := 0
	if pageToken != "" {
		parsed, err := strconv.Atoi(pageToken)
		if err != nil || parsed < 0 {
			return nil, "", errors.New("invalid permission page token")
		}
		offset = parsed
	}
	resourceIDs := make([]string, 0, len(claims.Resources))
	for _, resource := range claims.Resources {
		if filter == "" || filter == "urc" || filter == "urc-*" || filter == resource.ResourceID {
			resourceIDs = append(resourceIDs, resource.ResourceID)
		}
	}
	if filter != "" && filter != "urc" && filter != "urc-*" && !authz.ValidResourceID(filter) {
		return nil, "", authz.ErrInvalidResource
	}
	if offset > len(resourceIDs) {
		return nil, "", errors.New("invalid permission page token")
	}
	end := offset + pageSize
	if end > len(resourceIDs) {
		end = len(resourceIDs)
	}
	result := make([]authz.ResourcePermissions, 0, end-offset)
	for _, resourceID := range resourceIDs[offset:end] {
		narrowed, ok := service.currentTokenPermissions(ctx, claims, resourceID)
		if !ok {
			continue
		}
		result = append(result, authz.ResourcePermissions{
			ResourceID:  resourceID,
			Permissions: authz.PermissionList(narrowed),
		})
	}
	next := ""
	if end < len(resourceIDs) {
		next = strconv.Itoa(end)
	}
	return result, next, nil
}

func (service *Service) lookupAuthenticationPermissions(
	ctx context.Context,
	claims LoreClaims,
	filter string,
	pageSize int,
	pageToken string,
) ([]authz.ResourcePermissions, string, error) {
	if claims.IsServiceAccount {
		if !authz.ValidResourceID(filter) || pageToken != "" {
			return nil, "", errors.New("service principal repository enumeration is unavailable")
		}
		permissions, ok := service.authenticationPermissions(ctx, claims, filter)
		if !ok {
			return nil, "", nil
		}
		return []authz.ResourcePermissions{{ResourceID: filter, Permissions: permissions}}, "", nil
	}
	resources, next, err := service.policy.ListResourcePermissions(
		ctx,
		claims.Subject,
		filter,
		pageSize,
		pageToken,
	)
	if err != nil {
		return nil, "", err
	}
	filtered := resources[:0]
	for _, resource := range resources {
		permissions, ok := permissionsForAuthenticationToken(claims, resource.Permissions)
		if !ok {
			continue
		}
		resource.Permissions = permissions
		filtered = append(filtered, resource)
	}
	return filtered, next, nil
}

func (service *Service) authenticationPermissions(
	ctx context.Context,
	claims LoreClaims,
	resourceID string,
) ([]string, bool) {
	var permissions []string
	if claims.IsServiceAccount {
		policy, ok := service.policy.(servicePrincipalPolicy)
		if !ok {
			return nil, false
		}
		principal, granted, err := policy.ServicePrincipalResource(
			ctx, claims.PreferredUsername, resourceID,
		)
		if err != nil || principal.ID != claims.Subject {
			return nil, false
		}
		permissions = granted
	} else {
		current, err := service.policy.EffectivePermissions(ctx, claims.Subject, resourceID)
		if err != nil {
			return nil, false
		}
		permissions = current.Permissions
	}
	available := authz.ExpandPermissions(permissionMap(permissions))
	if len(claims.TokenScopes) != 0 {
		narrowed, ok := permissionsForAuthenticationToken(claims, authz.PermissionList(available))
		if !ok {
			return nil, false
		}
		available = authz.ExpandPermissions(permissionMap(narrowed))
	}
	if !available[authz.PermissionRead] {
		return nil, false
	}
	return authz.PermissionList(available), true
}
