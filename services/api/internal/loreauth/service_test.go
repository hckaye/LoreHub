package loreauth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/authz"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const testResource = "urc-0123456789abcdef0123456789abcdef"

type fakeAuthPolicy struct {
	mu               sync.RWMutex
	users            map[string]authz.UserInfo
	resources        map[string]map[string][]string
	serviceUsers     map[string]authz.UserInfo
	serviceResources map[string]map[string][]string
}

func (policy *fakeAuthPolicy) EffectivePermissions(
	_ context.Context,
	userID string,
	resourceID string,
) (authz.ResourcePermissions, error) {
	policy.mu.RLock()
	defer policy.mu.RUnlock()
	permissions := policy.resources[userID][resourceID]
	return authz.ResourcePermissions{ResourceID: resourceID, Permissions: append([]string(nil), permissions...)}, nil
}

func (policy *fakeAuthPolicy) ListResourcePermissions(
	_ context.Context,
	userID string,
	filter string,
	pageSize int,
	pageToken string,
) ([]authz.ResourcePermissions, string, error) {
	policy.mu.RLock()
	defer policy.mu.RUnlock()
	start := 0
	if pageToken != "" {
		if _, err := fmt.Sscanf(pageToken, "%d", &start); err != nil {
			return nil, "", err
		}
	}
	result := make([]authz.ResourcePermissions, 0)
	for resourceID, permissions := range policy.resources[userID] {
		if filter != "urc" && filter != "urc-*" && filter != "" && filter != resourceID {
			continue
		}
		result = append(result, authz.ResourcePermissions{ResourceID: resourceID, Permissions: permissions})
	}
	if start >= len(result) {
		return nil, "", nil
	}
	if pageSize < 1 {
		pageSize = 50
	}
	end := start + pageSize
	if end > len(result) {
		end = len(result)
	}
	next := ""
	if end < len(result) {
		next = fmt.Sprintf("%d", end)
	}
	return result[start:end], next, nil
}

func (policy *fakeAuthPolicy) UserInfo(_ context.Context, userID string) (authz.UserInfo, error) {
	policy.mu.RLock()
	defer policy.mu.RUnlock()
	user, ok := policy.users[userID]
	if !ok {
		return authz.UserInfo{}, errors.New("unknown user")
	}
	return user, nil
}

func (policy *fakeAuthPolicy) ServicePrincipalResource(
	_ context.Context,
	name string,
	resourceID string,
) (authz.UserInfo, []string, error) {
	policy.mu.RLock()
	defer policy.mu.RUnlock()
	principal, ok := policy.serviceUsers[name]
	if !ok {
		return authz.UserInfo{}, nil, errors.New("unknown service principal")
	}
	permissions := policy.serviceResources[name][resourceID]
	if len(permissions) == 0 {
		return authz.UserInfo{}, nil, errors.New("service principal has no grant")
	}
	return principal, append([]string(nil), permissions...), nil
}

func (policy *fakeAuthPolicy) UserInfoForResource(
	_ context.Context,
	resourceID string,
	userIDs []string,
) ([]authz.UserInfo, error) {
	policy.mu.RLock()
	defer policy.mu.RUnlock()
	result := make([]authz.UserInfo, 0, len(userIDs))
	for _, userID := range userIDs {
		if _, ok := policy.resources[userID][resourceID]; ok {
			result = append(result, policy.users[userID])
		}
	}
	return result, nil
}

func (policy *fakeAuthPolicy) UserInfoByDisplayName(
	_ context.Context,
	resourceID string,
	displayName string,
) (authz.UserInfo, error) {
	policy.mu.RLock()
	defer policy.mu.RUnlock()
	for userID, user := range policy.users {
		if user.DisplayName == displayName {
			if _, ok := policy.resources[userID][resourceID]; ok {
				return user, nil
			}
		}
	}
	return authz.UserInfo{}, errors.New("unknown user")
}

func (*fakeAuthPolicy) ProviderSubject(context.Context, string) (string, error) {
	return "provider-subject", nil
}

func (*fakeAuthPolicy) CheckPolicy(context.Context, authz.PolicyCheck) (authz.PolicyDecision, error) {
	return authz.PolicyDecision{Allowed: true}, nil
}

type fakeSession struct {
	code       []byte
	state      []byte
	userID     string
	expiresAt  time.Time
	nextPollAt time.Time
	consumed   bool
}

type fakeSessions struct {
	mu       sync.Mutex
	sessions map[string]*fakeSession
}

func (sessions *fakeSessions) CreateLoreAuthSession(
	_ context.Context,
	id string,
	codeDigest []byte,
	stateDigest []byte,
	expiresAt time.Time,
) error {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.sessions[string(codeDigest)] = &fakeSession{
		code: append([]byte(nil), codeDigest...), state: append([]byte(nil), stateDigest...), expiresAt: expiresAt,
	}
	return nil
}

func (sessions *fakeSessions) ConfirmLoreAuthSession(_ context.Context, codeDigest []byte, userID string) error {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	session, ok := sessions.sessions[string(codeDigest)]
	if !ok || session.consumed || !session.expiresAt.After(time.Now()) {
		return authz.ErrSessionNotFound
	}
	session.userID = userID
	session.nextPollAt = time.Time{}
	return nil
}

func (sessions *fakeSessions) PollLoreAuthSession(
	_ context.Context,
	codeDigest []byte,
	stateDigest []byte,
) (authz.AuthSessionPoll, error) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	session, ok := sessions.sessions[string(codeDigest)]
	if !ok || !session.expiresAt.After(time.Now()) {
		return authz.AuthSessionPoll{}, authz.ErrSessionNotFound
	}
	if !bytes.Equal(session.state, stateDigest) {
		return authz.AuthSessionPoll{}, authz.ErrSessionState
	}
	if session.consumed {
		return authz.AuthSessionPoll{}, authz.ErrSessionConsumed
	}
	if session.nextPollAt.After(time.Now()) {
		return authz.AuthSessionPoll{}, authz.ErrSessionRateLimited
	}
	session.nextPollAt = time.Now().Add(time.Second)
	if session.userID == "" {
		return authz.AuthSessionPoll{}, nil
	}
	session.consumed = true
	return authz.AuthSessionPoll{Ready: true, UserID: session.userID}, nil
}

func newTestService(t *testing.T, policy *fakeAuthPolicy, sessions *fakeSessions) (*Service, *TokenService) {
	t.Helper()
	tokens, _ := newTestTokenService(t)
	service, err := NewService(policy, sessions, tokens, "https://app.example/auth/lore/confirm",
		"ucs-auth://auth.example:8443", 5*time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	return service, tokens
}

func TestNewServiceRejectsNonCanonicalAuthURL(t *testing.T) {
	tokens, _ := newTestTokenService(t)
	for _, authURL := range []string{
		"ucs-auth://auth.example:8443/path",
		"ucs-auth://auth.example:8443?tenant=one",
		"ucs-auth://user:password@auth.example:8443",
	} {
		_, err := NewService(testPolicy(), &fakeSessions{sessions: make(map[string]*fakeSession)}, tokens,
			"https://app.example/auth/lore/confirm", authURL, 5*time.Minute, false)
		if err == nil {
			t.Fatalf("accepted non-canonical Lore AuthURL %q", authURL)
		}
	}
}

func bearerContext(raw string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+raw))
}

func TestBrowserSessionBindsClientStateAndConsumesOnce(t *testing.T) {
	policy := testPolicy()
	sessions := &fakeSessions{sessions: make(map[string]*fakeSession)}
	service, _ := newTestService(t, policy, sessions)
	start, err := service.StartAuthSession(context.Background(), &StartAuthSessionRequest{
		ClientState: "client-state-with-enough-entropy",
	})
	if err != nil || len(start.SessionCode) < 32 || start.LoginUrl == "" {
		t.Fatalf("unexpected auth start: response=%#v error=%v", start, err)
	}
	if _, err := service.GetAuthSession(context.Background(), &GetAuthSessionRequest{
		SessionCode: start.SessionCode, ClientState: "wrong-client-state-with-enough-entropy",
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("state mismatch error = %v", err)
	}
	initial, err := service.GetAuthSession(context.Background(), &GetAuthSessionRequest{
		SessionCode: start.SessionCode, ClientState: "client-state-with-enough-entropy",
	})
	if err != nil || initial.UserToken != nil {
		t.Fatalf("unexpected pending poll: response=%#v error=%v", initial, err)
	}
	if err := service.ConfirmSession(context.Background(), start.SessionCode, "alice"); err != nil {
		t.Fatal(err)
	}
	ready, err := service.GetAuthSession(context.Background(), &GetAuthSessionRequest{
		SessionCode: start.SessionCode, ClientState: "client-state-with-enough-entropy",
	})
	if err != nil || ready.UserToken == nil {
		t.Fatalf("unexpected ready poll: response=%#v error=%v", ready, err)
	}
	claims, err := service.tokens.VerifyAuthenticationToken(ready.UserToken.UserToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims.Claims.Resources) != 0 || ready.UserToken.ExpiresAt < 1_000_000_000_000 {
		t.Fatalf("ready token is not a millisecond base token: claims=%#v token=%d",
			claims.Claims, ready.UserToken.ExpiresAt)
	}
	if _, err := service.tokens.VerifyResourceToken(ready.UserToken.UserToken); err == nil {
		t.Fatal("zero-resource authentication token must not verify as a resource token")
	}
	if _, err := service.GetAuthSession(context.Background(), &GetAuthSessionRequest{
		SessionCode: start.SessionCode, ClientState: "client-state-with-enough-entropy",
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("replayed poll error = %v", err)
	}
}

func TestSessionPollingIsRateLimitedAndConcurrentCompletionIsSingleUse(t *testing.T) {
	policy := testPolicy()
	sessions := &fakeSessions{sessions: make(map[string]*fakeSession)}
	service, _ := newTestService(t, policy, sessions)
	start, err := service.StartAuthSession(context.Background(), &StartAuthSessionRequest{
		ClientState: "client-state-with-enough-entropy",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.GetAuthSession(context.Background(), &GetAuthSessionRequest{
		SessionCode: start.SessionCode, ClientState: "client-state-with-enough-entropy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetAuthSession(context.Background(), &GetAuthSessionRequest{
		SessionCode: start.SessionCode, ClientState: "client-state-with-enough-entropy",
	}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("rate limit error = %v", err)
	}
	if err := service.ConfirmSession(context.Background(), start.SessionCode, "alice"); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, pollErr := service.GetAuthSession(context.Background(), &GetAuthSessionRequest{
				SessionCode: start.SessionCode, ClientState: "client-state-with-enough-entropy",
			})
			results <- pollErr
		}()
	}
	var successes int
	for range 2 {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent polls = %d, want one", successes)
	}
}

func TestAuthenticationTokenDoesNotEnumerateResourcesForUserWithoutRepositories(t *testing.T) {
	policy := testPolicy()
	policy.users["empty"] = authz.UserInfo{ID: "empty", Username: "empty", DisplayName: "空の利用者"}
	policy.resources["empty"] = map[string][]string{}
	sessions := &fakeSessions{sessions: make(map[string]*fakeSession)}
	service, tokens := newTestService(t, policy, sessions)
	start, err := service.StartAuthSession(context.Background(), &StartAuthSessionRequest{
		ClientState: "empty-user-client-state-entropy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ConfirmSession(context.Background(), start.SessionCode, "empty"); err != nil {
		t.Fatal(err)
	}
	response, err := service.GetAuthSession(context.Background(), &GetAuthSessionRequest{
		SessionCode: start.SessionCode, ClientState: "empty-user-client-state-entropy",
	})
	if err != nil || response.UserToken == nil {
		t.Fatalf("empty-user login failed: response=%#v error=%v", response, err)
	}
	if response.UserToken.ExpiresAt < 1_000_000_000_000 {
		t.Fatalf("expected Unix milliseconds, got %d", response.UserToken.ExpiresAt)
	}
	verified, err := tokens.VerifyAuthenticationToken(response.UserToken.UserToken)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Claims.Subject != "empty" || len(verified.Claims.Resources) != 0 {
		t.Fatalf("empty-user base token claims = %#v", verified.Claims)
	}
	if _, err := tokens.VerifyResourceToken(response.UserToken.UserToken); err == nil {
		t.Fatal("base authentication token must not be usable as a data-plane token")
	}
}

func TestServiceCredentialIsTypedAndBindsSubjectScopeAndExpiry(t *testing.T) {
	policy := testPolicy()
	const subject = "00000000-0000-4000-8000-000000000001"
	policy.serviceUsers = map[string]authz.UserInfo{
		"lorehub-anonymous-reader": {
			ID: subject, Username: "lorehub-anonymous-reader", DisplayName: "Public reader",
		},
	}
	policy.serviceResources = map[string]map[string][]string{
		"lorehub-anonymous-reader": {
			testResource: {authz.PermissionRead},
		},
	}
	service, tokens := newTestService(t, policy, &fakeSessions{sessions: make(map[string]*fakeSession)})
	principal := loreclient.ServicePrincipal(loreclient.ServicePurposePublicReader, subject)
	credential, err := service.IssueServiceResourceToken(
		context.Background(), principal, testResource, []string{authz.PermissionRead},
	)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Token == "" || credential.AuthenticationToken == "" || credential.AuthURL == "" ||
		credential.ExpiresAt.Before(time.Now()) || credential.AuthenticationExpiresAt.Before(time.Now()) ||
		credential.Subject != subject || credential.Identity != subject || credential.ResourceID != testResource ||
		credential.Principal != principal || len(credential.RequestedScopes) != 1 ||
		len(credential.GrantedScopes) != 1 {
		t.Fatalf("service credential is not an exact typed credential: %+v", credential)
	}
	verified, err := tokens.VerifyResourceToken(credential.Token)
	if err != nil || verified.Claims.Subject != subject || !verified.Claims.IsServiceAccount ||
		len(verified.Claims.Resources) != 1 || verified.Claims.Resources[0].ResourceID != testResource {
		t.Fatalf("service credential claims are not bound: claims=%#v error=%v", verified.Claims, err)
	}
	base, err := tokens.VerifyAuthenticationToken(credential.AuthenticationToken)
	if err != nil || base.Claims.Subject != subject || !base.Claims.IsServiceAccount ||
		len(base.Claims.Resources) != 0 {
		t.Fatalf("service credential authentication claims are not bound: claims=%#v error=%v",
			base.Claims, err)
	}
	if _, err := service.IssueServiceResourceToken(context.Background(),
		loreclient.ServicePrincipal(loreclient.ServicePurposePublicReader, "wrong-subject"),
		testResource, []string{authz.PermissionRead}); !errors.Is(err, loreclient.ErrCredentialContract) {
		t.Fatalf("wrong service subject error = %v, want contract rejection", err)
	}
	service.tokens.lifetime = -time.Second
	if _, err := service.IssueServiceResourceToken(context.Background(), principal, testResource,
		[]string{authz.PermissionRead}); err == nil {
		t.Fatal("expired service credential was accepted")
	}
}

func TestServiceAuthenticationTokenExchangesToServiceResourceToken(t *testing.T) {
	policy := testPolicy()
	const subject = "00000000-0000-4000-8000-000000000001"
	policy.serviceUsers = map[string]authz.UserInfo{
		"lorehub-anonymous-reader": {
			ID: subject, Username: "lorehub-anonymous-reader", DisplayName: "Public reader",
		},
	}
	policy.serviceResources = map[string]map[string][]string{
		"lorehub-anonymous-reader": {testResource: {authz.PermissionRead}},
	}
	service, tokens := newTestService(t, policy, &fakeSessions{sessions: make(map[string]*fakeSession)})
	credential, err := service.IssueServiceResourceToken(context.Background(),
		loreclient.ServicePrincipal(loreclient.ServicePurposePublicReader, subject), testResource,
		[]string{authz.PermissionRead})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.ExchangeUserTokenForMultiresourceToken(
		bearerContext(credential.AuthenticationToken),
		&ExchangeUserTokenForMultiresourceTokenRequest{ResourceId: []string{testResource}},
	)
	if err != nil || response.Token == nil {
		t.Fatalf("service exchange failed: response=%#v error=%v", response, err)
	}
	verified, err := tokens.VerifyResourceToken(response.Token.UserToken)
	if err != nil || verified.Claims.Subject != subject || !verified.Claims.IsServiceAccount {
		t.Fatalf("service exchange lost service principal binding: claims=%#v error=%v",
			verified.Claims, err)
	}
	policy.mu.Lock()
	policy.serviceResources["lorehub-anonymous-reader"][testResource] = nil
	policy.mu.Unlock()
	if _, err := service.ExchangeUserTokenForMultiresourceToken(
		bearerContext(credential.AuthenticationToken),
		&ExchangeUserTokenForMultiresourceTokenRequest{ResourceId: []string{testResource}},
	); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("revoked service grant error = %v, want PermissionDenied", err)
	}
}

func TestExchangeCannotWidenResourceScope(t *testing.T) {
	policy := testPolicy()
	sessions := &fakeSessions{sessions: make(map[string]*fakeSession)}
	service, tokens := newTestService(t, policy, sessions)
	raw, _, err := tokens.MintAuthenticationToken(policy.users["alice"])
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.ExchangeUserTokenForMultiresourceToken(bearerContext(raw),
		&ExchangeUserTokenForMultiresourceTokenRequest{ResourceId: []string{testResource}})
	if err != nil || response.Token == nil {
		t.Fatalf("read exchange failed: response=%#v error=%v", response, err)
	}
	if response.Token.ExpiresAt < 1_000_000_000_000 {
		t.Fatalf("resource token expiry is not Unix milliseconds: %d", response.Token.ExpiresAt)
	}
	if _, err := service.GetUserId(bearerContext(raw), &GetUserIdRequest{
		ResourceId: testResource, UserDisplayName: "Alice",
	}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("base authentication token reached data-plane user lookup: %v", err)
	}
	if _, err := service.ExchangeUserTokenForMultiresourceToken(bearerContext(raw),
		&ExchangeUserTokenForMultiresourceTokenRequest{ResourceId: []string{
			"urc-fedcba9876543210fedcba9876543210",
		}}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross-resource exchange error = %v", err)
	}
	external, err := service.ExchangeExternalTokenForUserToken(context.Background(),
		&ExchangeExternalTokenForUserTokenRequest{
			ExternalToken: raw,
			TokenType:     loreclient.AuthenticationTokenType,
		})
	if err != nil || external.UserToken == nil || external.UserToken.UserToken != raw ||
		external.UserToken.UserId != "alice" || external.UserToken.ExpiresAt < 1_000_000_000_000 {
		t.Fatalf("external base-token exchange failed: response=%#v error=%v", external, err)
	}
	resourceToken := response.Token.UserToken
	if _, err := service.ExchangeExternalTokenForUserToken(context.Background(),
		&ExchangeExternalTokenForUserTokenRequest{
			ExternalToken: resourceToken,
			TokenType:     loreclient.AuthenticationTokenType,
		}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("resource token external exchange error = %v", err)
	}
	if _, err := service.ExchangeExternalTokenForUserToken(context.Background(),
		&ExchangeExternalTokenForUserTokenRequest{
			ExternalToken: raw,
			TokenType:     "lore",
		}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unsupported external token type error = %v", err)
	}
}

func TestAuthenticationTokenDiscoversOnlyCurrentRepositories(t *testing.T) {
	policy := testPolicy()
	service, tokens := newTestService(t, policy, &fakeSessions{sessions: make(map[string]*fakeSession)})
	baseToken, _, err := tokens.MintAuthenticationToken(policy.users["alice"])
	if err != nil {
		t.Fatal(err)
	}
	permissions, err := service.CheckUserPermission(
		bearerContext(baseToken),
		&CheckUserPermissionRequest{ResourceId: []string{testResource}},
	)
	if err != nil || len(permissions.AllowedResourcePermission) != 1 ||
		len(permissions.DeniedResourcePermission) != 0 {
		t.Fatalf("repository lookup permission failed: response=%#v error=%v", permissions, err)
	}
	pageSize := int32(50)
	listed, err := service.LookupUserPermissions(
		bearerContext(baseToken),
		&LookupUserPermissionsRequest{ResourceFilter: "urc", PageSize: &pageSize},
	)
	if err != nil || len(listed.ResourcePermission) != 1 ||
		listed.ResourcePermission[0].ResourceId != testResource {
		t.Fatalf("repository list permission failed: response=%#v error=%v", listed, err)
	}
	if _, err := service.GetUserId(bearerContext(baseToken), &GetUserIdRequest{
		ResourceId: testResource, UserDisplayName: "Alice",
	}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("authentication token reached repository data lookup: %v", err)
	}
	policy.mu.Lock()
	delete(policy.resources["alice"], testResource)
	policy.mu.Unlock()
	permissions, err = service.CheckUserPermission(
		bearerContext(baseToken),
		&CheckUserPermissionRequest{ResourceId: []string{testResource}},
	)
	if err != nil || len(permissions.AllowedResourcePermission) != 0 ||
		len(permissions.DeniedResourcePermission) != 1 {
		t.Fatalf("revoked repository permission was accepted: response=%#v error=%v", permissions, err)
	}
}

func TestRebacResourceLifecycleRequiresScopedAdmin(t *testing.T) {
	policy := testPolicy()
	policy.users["manager"] = authz.UserInfo{ID: "manager", Username: "manager", DisplayName: "Manager"}
	policy.resources["manager"] = map[string][]string{
		testResource: {authz.PermissionRead, authz.PermissionWrite, authz.PermissionAdmin},
	}
	sessions := &fakeSessions{sessions: make(map[string]*fakeSession)}
	service, tokens := newTestService(t, policy, sessions)
	rebac, err := NewRebacService(service)
	if err != nil {
		t.Fatal(err)
	}
	readWrite, _, err := tokens.MintResourceToken(policy.users["alice"], []LoreResourcePermission{{
		ResourceID: testResource, Permission: []string{authz.PermissionRead, authz.PermissionWrite},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rebac.CreateResource(bearerContext(readWrite), &CreateResourceRequest{
		ResourceId: testResource, ResourceName: "partition-a",
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("read/write resource creation error = %v", err)
	}
	admin, _, err := tokens.MintResourceToken(policy.users["manager"], []LoreResourcePermission{{
		ResourceID: testResource, Permission: []string{authz.PermissionAdmin},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rebac.CreateResource(bearerContext(admin), &CreateResourceRequest{
		ResourceId: testResource, ResourceName: "partition-a",
	}); err != nil {
		t.Fatalf("admin resource creation error = %v", err)
	}
}

func TestUnicodeUserInfoAndPermissionLookup(t *testing.T) {
	policy := testPolicy()
	policy.users["alice"] = authz.UserInfo{ID: "alice", Username: "alice", DisplayName: "アリス 🚀"}
	sessions := &fakeSessions{sessions: make(map[string]*fakeSession)}
	service, tokens := newTestService(t, policy, sessions)
	raw, _, err := tokens.MintResourceToken(policy.users["alice"], []LoreResourcePermission{{
		ResourceID: testResource, Permission: []string{authz.PermissionRead},
	}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.GetUserId(bearerContext(raw), &GetUserIdRequest{
		ResourceId: testResource, UserDisplayName: "アリス 🚀",
	})
	if err != nil || response.UserInfo == nil || response.UserInfo.DisplayName != "アリス 🚀" {
		t.Fatalf("unicode user lookup failed: response=%#v error=%v", response, err)
	}
}

func TestRevokedUserOrResourceCannotUseAnExistingResourceToken(t *testing.T) {
	policy := testPolicy()
	sessions := &fakeSessions{sessions: make(map[string]*fakeSession)}
	service, tokens := newTestService(t, policy, sessions)
	raw, _, err := tokens.MintResourceToken(policy.users["alice"], []LoreResourcePermission{{
		ResourceID: testResource, Permission: []string{authz.PermissionRead},
	}})
	if err != nil {
		t.Fatal(err)
	}
	policy.mu.Lock()
	delete(policy.resources["alice"], testResource)
	policy.mu.Unlock()
	if _, err := service.GetUserId(bearerContext(raw), &GetUserIdRequest{
		ResourceId: testResource, UserDisplayName: "Alice",
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("revoked resource token was accepted: %v", err)
	}

	policy = testPolicy()
	service, tokens = newTestService(t, policy, sessions)
	raw, _, err = tokens.MintResourceToken(policy.users["alice"], []LoreResourcePermission{{
		ResourceID: testResource, Permission: []string{authz.PermissionRead},
	}})
	if err != nil {
		t.Fatal(err)
	}
	policy.mu.Lock()
	delete(policy.users, "alice")
	policy.mu.Unlock()
	if _, err := service.GetProviderUserId(
		bearerContext(raw), &GetProviderUserIdRequest{UserId: "alice"},
	); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("inactive user token was accepted: %v", err)
	}
}

func testPolicy() *fakeAuthPolicy {
	return &fakeAuthPolicy{
		users: map[string]authz.UserInfo{
			"alice": {ID: "alice", Username: "alice", DisplayName: "Alice"},
		},
		resources: map[string]map[string][]string{
			"alice": {testResource: {authz.PermissionRead, authz.PermissionWrite}},
		},
		serviceUsers:     make(map[string]authz.UserInfo),
		serviceResources: make(map[string]map[string][]string),
	}
}
