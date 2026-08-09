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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const testResource = "urc-0123456789abcdef0123456789abcdef"

type fakeAuthPolicy struct {
	mu        sync.RWMutex
	users     map[string]authz.UserInfo
	resources map[string]map[string][]string
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
	if _, err := service.tokens.Verify(ready.UserToken.UserToken); err != nil {
		t.Fatal(err)
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

func TestExchangeCannotWidenResourceScope(t *testing.T) {
	policy := testPolicy()
	sessions := &fakeSessions{sessions: make(map[string]*fakeSession)}
	service, tokens := newTestService(t, policy, sessions)
	raw, _, err := tokens.MintResourceToken(policy.users["alice"], []LoreResourcePermission{{
		ResourceID: testResource, Permission: []string{authz.PermissionRead},
	}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.ExchangeUserTokenForMultiresourceToken(bearerContext(raw),
		&ExchangeUserTokenForMultiresourceTokenRequest{ResourceId: []string{testResource}})
	if err != nil || response.Token == nil {
		t.Fatalf("read exchange failed: response=%#v error=%v", response, err)
	}
	if _, err := service.ExchangeUserTokenForMultiresourceToken(bearerContext(raw),
		&ExchangeUserTokenForMultiresourceTokenRequest{ResourceId: []string{
			"urc-fedcba9876543210fedcba9876543210",
		}}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross-resource exchange error = %v", err)
	}
	if _, err := service.ExchangeExternalTokenForUserToken(
		context.Background(), &ExchangeExternalTokenForUserTokenRequest{},
	); status.Code(err) != codes.Unimplemented {
		t.Fatalf("external token exchange error = %v", err)
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

func testPolicy() *fakeAuthPolicy {
	return &fakeAuthPolicy{
		users: map[string]authz.UserInfo{
			"alice": {ID: "alice", Username: "alice", DisplayName: "Alice"},
		},
		resources: map[string]map[string][]string{
			"alice": {testResource: {authz.PermissionRead, authz.PermissionWrite}},
		},
	}
}
