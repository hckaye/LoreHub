package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
	"github.com/lorehub/lorehub/services/api/internal/runner"
)

type fakeRunnerStore struct {
	createdInput   platform.CreateRunnerRegistrationTokenInput
	consumedDigest []byte
	registerInput  platform.RegisterRunnerInput
	listedActor    platform.User
	listedScope    platform.RunnerScope
	revokedActor   platform.User
	revokedScope   platform.RunnerScope
	revokedID      string
	registration   platform.RunnerRegistrationToken
	runners        []platform.Runner
	consumeErr     error
	registerErr    error
	authenticated  platform.Runner
}

func (store *fakeRunnerStore) CreateRegistrationToken(
	_ context.Context,
	_ platform.User,
	input platform.CreateRunnerRegistrationTokenInput,
) (platform.RunnerRegistrationToken, error) {
	store.createdInput = input
	return platform.RunnerRegistrationToken{
		ID: uuid.NewString(), Scope: input.Scope, ExpiresAt: input.ExpiresAt,
	}, nil
}

func (store *fakeRunnerStore) ConsumeRegistrationToken(
	_ context.Context,
	digest []byte,
) (platform.RunnerRegistrationToken, error) {
	store.consumedDigest = append([]byte(nil), digest...)
	return store.registration, store.consumeErr
}

func (store *fakeRunnerStore) RegisterRunner(
	_ context.Context,
	input platform.RegisterRunnerInput,
) (platform.Runner, error) {
	store.registerInput = input
	if store.registerErr != nil {
		return platform.Runner{}, store.registerErr
	}
	return platform.Runner{
		ID:                  "runner-1",
		Scope:               store.registration.Scope,
		Name:                input.Name,
		Labels:              input.Labels,
		CredentialExpiresAt: input.CredentialExpiresAt,
		RunnerVersion:       input.RunnerVersion,
		CreatedAt:           time.Now().UTC(),
	}, nil
}

func (store *fakeRunnerStore) ListRunners(
	_ context.Context,
	actor platform.User,
	scope platform.RunnerScope,
) ([]platform.Runner, error) {
	store.listedActor = actor
	store.listedScope = scope
	return append([]platform.Runner(nil), store.runners...), nil
}

func (store *fakeRunnerStore) RevokeRunner(
	_ context.Context,
	actor platform.User,
	scope platform.RunnerScope,
	runnerID string,
) error {
	store.revokedActor = actor
	store.revokedScope = scope
	store.revokedID = runnerID
	return nil
}

func (*fakeRunnerStore) TouchRunnerSeen(context.Context, string, time.Time) error { return nil }

func (store *fakeRunnerStore) AuthenticateRunner(
	context.Context, []byte, string, time.Time,
) (platform.Runner, error) {
	return store.authenticated, nil
}

type rejectingRunnerEndpointAuthenticator struct {
	called bool
}

func (authenticator *rejectingRunnerEndpointAuthenticator) Authenticate(
	context.Context,
	string,
) (auth.Principal, error) {
	authenticator.called = true
	return auth.Principal{}, errors.New("session authentication must not be used")
}

func TestRunnerRegisterEndpointExchangesRegistrationToken(t *testing.T) {
	codec, err := auth.NewSecretCodec("runner HTTP registration test secret")
	if err != nil {
		t.Fatal(err)
	}
	rawRegistrationToken, _, err := auth.NewRunnerRegistrationToken(codec)
	if err != nil {
		t.Fatal(err)
	}
	runnerStore := &fakeRunnerStore{registration: platform.RunnerRegistrationToken{
		ID: uuid.NewString(),
		Scope: platform.RunnerScope{
			OrganizationID: uuid.NewString(),
			RepositoryID:   uuid.NewString(),
		},
	}}
	authenticator := &rejectingRunnerEndpointAuthenticator{}
	handler := New(
		fakeStore{}, fakeLore{}, authenticator, healthy{}, "",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithRunners(runnerStore, codec, "runner-key-v1"),
	)
	body, _ := json.Marshal(map[string]any{
		"name": "Linux builder", "labels": []string{"self-hosted", "Linux"}, "version": "1.2.3",
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/actions/runner/register", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+rawRegistrationToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("register runner status = %d, body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Runner platform.Runner `json:"runner"`
		Token  string          `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Runner.ID != "runner-1" || result.Runner.Name != "Linux builder" ||
		!auth.ValidRunnerCredential(result.Token) {
		t.Fatalf("unexpected runner registration response: %+v", result)
	}
	if !bytes.Equal(runnerStore.consumedDigest, codec.Digest(rawRegistrationToken)) {
		t.Fatal("registration endpoint did not digest the presented registration token")
	}
	if runnerStore.registerInput.RegistrationTokenID != runnerStore.registration.ID ||
		runnerStore.registerInput.CredentialKeyID != "runner-key-v1" ||
		runnerStore.registerInput.RunnerVersion != "1.2.3" ||
		!codec.Matches(result.Token, runnerStore.registerInput.CredentialDigest) {
		t.Fatalf("unexpected runner store input: %+v", runnerStore.registerInput)
	}
	if authenticator.called {
		t.Fatal("runner registration endpoint used browser or user authentication")
	}
}

func TestRunnerRegisterEndpointRejectsInvalidOrConsumedTokens(t *testing.T) {
	codec, _ := auth.NewSecretCodec("runner HTTP invalid token test secret")
	raw, _, err := auth.NewRunnerRegistrationToken(codec)
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name          string
		authorization string
		consumeErr    error
	}{
		{name: "missing token"},
		{name: "wrong prefix", authorization: "Bearer lhr_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"},
		{name: "consumed token", authorization: "Bearer " + raw, consumeErr: auth.ErrInvalidRunnerToken},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			store := &fakeRunnerStore{
				registration: platform.RunnerRegistrationToken{ID: uuid.NewString()},
				consumeErr:   testCase.consumeErr,
			}
			handler := New(
				fakeStore{}, fakeLore{}, &rejectingRunnerEndpointAuthenticator{}, healthy{}, "",
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				WithRunners(store, codec, "runner-key-v1"),
			)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/actions/runner/register",
				bytes.NewBufferString(`{"name":"runner","labels":["self-hosted"],"version":"1"}`))
			request.Header.Set("Authorization", testCase.authorization)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized ||
				response.Header().Get("WWW-Authenticate") == "" {
				t.Fatalf("invalid token status = %d, headers=%v, body=%s",
					response.Code, response.Header(), response.Body.String())
			}
		})
	}
}

func TestRunnerManagementRoutesResolveOrganizationAndRepositoryScopes(t *testing.T) {
	codec, _ := auth.NewSecretCodec("runner management HTTP test secret")
	actor := platform.User{ID: "user-1", Username: "alice"}
	organizationID := uuid.NewString()
	repositoryID := uuid.NewString()
	runnerStore := &fakeRunnerStore{runners: []platform.Runner{{ID: "runner-1"}}}
	identityStore := &fakeActionsContextIdentityStore{
		organization: platform.OrganizationView{ID: organizationID},
	}
	actionsStore := &fakeActionsContextRepositoryStore{access: runner.RepositoryAccess{
		ID: repositoryID, OrganizationID: organizationID, CanRead: true,
	}}
	handler := New(
		fakeStore{user: actor}, fakeLore{}, staticAuthenticator{principal: auth.Principal{Subject: "subject"}},
		healthy{}, "", slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithRunners(runnerStore, codec, "runner-key-v1"),
		WithIdentityStore(identityStore), WithActions(actionsStore),
	)

	organizationRequest := httptest.NewRequest(http.MethodGet,
		"/api/v1/organizations/acme/actions/runners", nil)
	organizationRequest.Header.Set("Authorization", "Bearer user-token")
	organizationResponse := httptest.NewRecorder()
	handler.ServeHTTP(organizationResponse, organizationRequest)
	if organizationResponse.Code != http.StatusOK ||
		runnerStore.listedScope != (platform.RunnerScope{OrganizationID: organizationID}) ||
		runnerStore.listedActor.ID != actor.ID {
		t.Fatalf("organization runner list status=%d actor=%+v scope=%+v body=%s",
			organizationResponse.Code, runnerStore.listedActor, runnerStore.listedScope,
			organizationResponse.Body.String())
	}

	repositoryRequest := httptest.NewRequest(http.MethodDelete,
		"/api/v1/repositories/acme/project/actions/runners/runner-7", nil)
	repositoryRequest.Header.Set("Authorization", "Bearer user-token")
	repositoryResponse := httptest.NewRecorder()
	handler.ServeHTTP(repositoryResponse, repositoryRequest)
	wantScope := platform.RunnerScope{OrganizationID: organizationID, RepositoryID: repositoryID}
	if repositoryResponse.Code != http.StatusNoContent || runnerStore.revokedScope != wantScope ||
		runnerStore.revokedActor.ID != actor.ID || runnerStore.revokedID != "runner-7" {
		t.Fatalf("repository runner revoke status=%d actor=%+v scope=%+v id=%q body=%s",
			repositoryResponse.Code, runnerStore.revokedActor, runnerStore.revokedScope,
			runnerStore.revokedID, repositoryResponse.Body.String())
	}
}
