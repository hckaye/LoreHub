package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
	"github.com/lorehub/lorehub/services/api/internal/runner"
)

const testSARIFUploadID = "ca30cfd4-bb49-4d9e-aa69-ca389f315d3a"

type fakeActionsSecurityStore struct {
	uploadInput runner.SARIFUploadInput
	upload      runner.SARIFUploadMetadata
	uploadErr   error
	uploads     []runner.SARIFUploadMetadata
	alerts      []runner.CodeScanningAlert
	selector    runner.SARIFRepositorySelector
	uploadID    string
	limit       int
}

func (store *fakeActionsSecurityStore) UploadSARIF(
	_ context.Context,
	input runner.SARIFUploadInput,
) (runner.SARIFUploadMetadata, error) {
	store.uploadInput = input
	return store.upload, store.uploadErr
}

func (store *fakeActionsSecurityStore) ListSARIFUploads(
	_ context.Context,
	selector runner.SARIFRepositorySelector,
	limit int,
) ([]runner.SARIFUploadMetadata, error) {
	store.selector = selector
	store.limit = limit
	return store.uploads, store.uploadErr
}

func (store *fakeActionsSecurityStore) GetSARIFUpload(
	_ context.Context,
	selector runner.SARIFRepositorySelector,
	uploadID string,
) (runner.SARIFUploadMetadata, error) {
	store.selector = selector
	store.uploadID = uploadID
	return store.upload, store.uploadErr
}

func (store *fakeActionsSecurityStore) ListCodeScanningAlerts(
	_ context.Context,
	selector runner.SARIFRepositorySelector,
	uploadID string,
	limit int,
) ([]runner.CodeScanningAlert, error) {
	store.selector = selector
	store.uploadID = uploadID
	store.limit = limit
	return store.alerts, store.uploadErr
}

type fakeJobTokenVerifier struct {
	verified     runner.VerifiedJobToken
	err          error
	token        string
	restScope    string
	graphqlScope string
	calls        int
}

func (verifier *fakeJobTokenVerifier) Verify(
	_ context.Context,
	rawToken string,
	restScope string,
	graphqlScope string,
) (runner.VerifiedJobToken, error) {
	verifier.calls++
	verifier.token = rawToken
	verifier.restScope = restScope
	verifier.graphqlScope = graphqlScope
	return verifier.verified, verifier.err
}

type securityActionsStore struct {
	*fakeActionsStore
	accessErr       error
	repositoryCalls int
	actorID         string
}

func (store *securityActionsStore) RepositoryForActions(
	_ context.Context,
	_ string,
	_ string,
	actorID string,
) (runner.RepositoryAccess, error) {
	store.repositoryCalls++
	store.actorID = actorID
	return store.access, store.accessErr
}

type codeScanningAuthenticator struct {
	calls int
}

func (authenticator *codeScanningAuthenticator) Authenticate(
	_ context.Context,
	authorization string,
) (auth.Principal, error) {
	authenticator.calls++
	if authorization != "Bearer user-token" {
		return auth.Principal{}, auth.ErrMissingToken
	}
	return auth.Principal{Subject: "user-subject"}, nil
}

func TestSARIFUploadUsesOnlyScopedJobTokenBoundary(t *testing.T) {
	document := []byte(`{"version":"2.1.0","runs":[]}`)
	securityStore := &fakeActionsSecurityStore{upload: runner.SARIFUploadMetadata{ID: testSARIFUploadID}}
	verifier := validFakeJobTokenVerifier()
	actions := &securityActionsStore{fakeActionsStore: &fakeActionsStore{}}
	authenticator := &codeScanningAuthenticator{}
	handler := newActionsSecurityHandler(actions, securityStore, verifier, authenticator)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v3/repos/acme/demo/code-scanning/sarifs",
		strings.NewReader(sarifUploadJSON(t, document, true)),
	)
	request.Header.Set("Authorization", "Bearer signed-job-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted || response.Header().Get("Location") !=
		"/api/v1/repositories/acme/demo/code-scanning/sarif-uploads/"+testSARIFUploadID {
		t.Fatalf("unexpected SARIF upload response: %d %s", response.Code, response.Body.String())
	}
	input := securityStore.uploadInput
	if input.Owner != "acme" || input.Repository != "demo" || input.ExpectedRevision != "revision-one" ||
		input.ExpectedRef != "refs/heads/main" || !bytes.Equal(input.Document, document) ||
		input.Claims.RepositoryID != verifier.verified.Claims.RepositoryID ||
		input.Claims.RunID != verifier.verified.Claims.RunID || input.Claims.JobID != verifier.verified.Claims.JobID ||
		input.Claims.Attempt != verifier.verified.Claims.Attempt {
		t.Fatalf("upload boundary was not retained exactly: %#v", input)
	}
	if verifier.token != "signed-job-token" || verifier.restScope != runner.JobTokenRESTScope ||
		verifier.graphqlScope != runner.JobTokenGraphQLScope || actions.repositoryCalls != 0 ||
		authenticator.calls != 0 {
		t.Fatalf("upload used the wrong authentication path: verifier=%#v actions=%d auth=%d",
			verifier, actions.repositoryCalls, authenticator.calls)
	}
}

func TestSARIFUploadAcceptsStrictBase64JSONAndRejectsUnsafePayloads(t *testing.T) {
	document := []byte(`{"version":"2.1.0","runs":[]}`)
	tests := []struct {
		name       string
		body       string
		auth       string
		verifyErr  error
		wantStatus int
	}{
		{name: "raw JSON", body: sarifUploadJSON(t, document, false), auth: "Bearer job", wantStatus: 202},
		{name: "missing bearer", body: sarifUploadJSON(t, document, false), wantStatus: 401},
		{name: "invalid token", body: sarifUploadJSON(t, document, false), auth: "Bearer job",
			verifyErr: runner.ErrActionsJobTokenInvalid, wantStatus: 401},
		{name: "scope denied", body: sarifUploadJSON(t, document, false), auth: "Bearer job",
			verifyErr: runner.ErrActionsJobTokenScope, wantStatus: 403},
		{name: "database state revoked", body: sarifUploadJSON(t, document, false), auth: "Bearer job",
			verifyErr: runner.ErrActionsJobTokenUnauthorized, wantStatus: 403},
		{name: "invalid base64", body: `{"commit_sha":"revision-one","ref":"refs/heads/main","sarif":"%%%"}`,
			auth: "Bearer job", wantStatus: 400},
		{name: "base64 whitespace", body: `{"commit_sha":"revision-one","ref":"refs/heads/main",` +
			`"sarif":"e3tc\nbl0="}`, auth: "Bearer job", wantStatus: 400},
		{name: "missing ref", body: `{"commit_sha":"revision-one","sarif":"e30="}`,
			auth: "Bearer job", wantStatus: 400},
		{name: "unknown field", body: `{"commit_sha":"revision-one","ref":"refs/heads/main",` +
			`"sarif":"e30=","secret":"no"}`, auth: "Bearer job", wantStatus: 400},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			securityStore := &fakeActionsSecurityStore{
				upload: runner.SARIFUploadMetadata{ID: testSARIFUploadID},
			}
			verifier := validFakeJobTokenVerifier()
			verifier.err = test.verifyErr
			handler := newActionsSecurityHandler(nil, securityStore, verifier, &codeScanningAuthenticator{})
			request := httptest.NewRequest(http.MethodPost,
				"/api/v3/repos/acme/demo/code-scanning/sarifs", strings.NewReader(test.body))
			if test.auth != "" {
				request.Header.Set("Authorization", test.auth)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}

	tooLarge := bytes.Repeat([]byte("x"), runner.MaxSARIFUploadBytes+1)
	securityStore := &fakeActionsSecurityStore{upload: runner.SARIFUploadMetadata{ID: testSARIFUploadID}}
	request := httptest.NewRequest(http.MethodPost, "/api/v3/repos/acme/demo/code-scanning/sarifs",
		strings.NewReader(sarifUploadJSON(t, tooLarge, true)))
	request.Header.Set("Authorization", "Bearer job")
	response := httptest.NewRecorder()
	newActionsSecurityHandler(nil, securityStore, validFakeJobTokenVerifier(),
		&codeScanningAuthenticator{}).ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge || securityStore.uploadInput.Document != nil {
		t.Fatalf("gzip expansion limit was not enforced: %d %s", response.Code, response.Body.String())
	}

	oversizedBody := `{"commit_sha":"revision-one","ref":"refs/heads/main","sarif":"` +
		strings.Repeat("A", maxSARIFRequestBody) + `"}`
	request = httptest.NewRequest(http.MethodPost, "/api/v3/repos/acme/demo/code-scanning/sarifs",
		strings.NewReader(oversizedBody))
	request.Header.Set("Authorization", "Bearer job")
	response = httptest.NewRecorder()
	newActionsSecurityHandler(nil, securityStore, validFakeJobTokenVerifier(),
		&codeScanningAuthenticator{}).ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("request body limit returned %d: %s", response.Code, response.Body.String())
	}
}

func TestSARIFUploadMapsStoreBoundaries(t *testing.T) {
	tests := []struct {
		err        error
		wantStatus int
	}{
		{err: runner.ErrSARIFNotFound, wantStatus: 404},
		{err: runner.ErrSARIFBoundary, wantStatus: 409},
		{err: runner.ErrSARIFInvalid, wantStatus: 422},
		{err: runner.ErrSARIFTooLarge, wantStatus: 413},
	}
	for _, test := range tests {
		store := &fakeActionsSecurityStore{uploadErr: test.err}
		request := httptest.NewRequest(http.MethodPost, "/api/v3/repos/acme/demo/code-scanning/sarifs",
			strings.NewReader(sarifUploadJSON(t, []byte(`{"version":"2.1.0","runs":[]}`), false)))
		request.Header.Set("Authorization", "Bearer job")
		response := httptest.NewRecorder()
		newActionsSecurityHandler(nil, store, validFakeJobTokenVerifier(),
			&codeScanningAuthenticator{}).ServeHTTP(response, request)
		if response.Code != test.wantStatus {
			t.Fatalf("error=%v status=%d want=%d", test.err, response.Code, test.wantStatus)
		}
	}
}

func TestCodeScanningReadsUseOptionalHumanActorPermissions(t *testing.T) {
	repositoryID := "8d52f102-d46e-40b0-91ae-0b7997ef8760"
	securityStore := &fakeActionsSecurityStore{
		upload:  runner.SARIFUploadMetadata{ID: testSARIFUploadID},
		uploads: []runner.SARIFUploadMetadata{{ID: testSARIFUploadID}},
		alerts:  []runner.CodeScanningAlert{{ID: "alert-one"}},
	}
	actions := &securityActionsStore{fakeActionsStore: &fakeActionsStore{
		access: runner.RepositoryAccess{ID: repositoryID, CanRead: true},
	}}
	authenticator := &codeScanningAuthenticator{}
	handler := newActionsSecurityHandler(actions, securityStore, validFakeJobTokenVerifier(), authenticator)
	paths := []string{
		"/api/v1/repositories/acme/demo/code-scanning/sarif-uploads?limit=10",
		"/api/v1/repositories/acme/demo/code-scanning/sarif-uploads/" + testSARIFUploadID,
		"/api/v1/repositories/acme/demo/code-scanning/alerts?upload_id=" + testSARIFUploadID +
			"&per_page=20",
	}
	for _, path := range paths {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("anonymous public read %s returned %d: %s", path, response.Code, response.Body.String())
		}
	}
	if actions.actorID != "" || authenticator.calls != 0 || securityStore.selector.RepositoryID != repositoryID ||
		securityStore.selector.Owner != "acme" || securityStore.selector.Repository != "demo" {
		t.Fatalf("anonymous selector was incorrect: actor=%q auth=%d selector=%#v",
			actions.actorID, authenticator.calls, securityStore.selector)
	}

	userRequest := httptest.NewRequest(http.MethodGet,
		"/api/v1/repositories/acme/demo/code-scanning/sarif-uploads", nil)
	userRequest.Header.Set("Authorization", "Bearer user-token")
	userResponse := httptest.NewRecorder()
	handler.ServeHTTP(userResponse, userRequest)
	if userResponse.Code != http.StatusOK || actions.actorID != "user-1" || authenticator.calls != 1 {
		t.Fatalf("authenticated read failed: %d actor=%q auth=%d",
			userResponse.Code, actions.actorID, authenticator.calls)
	}
}

func TestCodeScanningReadsRejectInvalidCredentialsAndHiddenRepositories(t *testing.T) {
	actions := &securityActionsStore{fakeActionsStore: &fakeActionsStore{
		access: runner.RepositoryAccess{ID: "repository-one", CanRead: false},
	}}
	securityStore := &fakeActionsSecurityStore{}
	verifier := validFakeJobTokenVerifier()
	handler := newActionsSecurityHandler(actions, securityStore, verifier, &codeScanningAuthenticator{})

	jobRequest := httptest.NewRequest(http.MethodGet,
		"/api/v1/repositories/acme/private/code-scanning/sarif-uploads", nil)
	jobRequest.Header.Set("Authorization", "Bearer job-token")
	jobResponse := httptest.NewRecorder()
	handler.ServeHTTP(jobResponse, jobRequest)
	if jobResponse.Code != http.StatusUnauthorized || verifier.calls != 0 {
		t.Fatalf("job token entered user read path: %d verifier=%d", jobResponse.Code, verifier.calls)
	}

	hiddenRequest := httptest.NewRequest(http.MethodGet,
		"/api/v1/repositories/acme/private/code-scanning/sarif-uploads", nil)
	hiddenResponse := httptest.NewRecorder()
	handler.ServeHTTP(hiddenResponse, hiddenRequest)
	if hiddenResponse.Code != http.StatusNotFound {
		t.Fatalf("hidden repository returned %d: %s", hiddenResponse.Code, hiddenResponse.Body.String())
	}
}

func newActionsSecurityHandler(
	actions ActionsStore,
	security ActionsSecurityStore,
	verifier runner.JobTokenVerifier,
	authenticator auth.Authenticator,
) http.Handler {
	if authenticator == nil {
		authenticator = &codeScanningAuthenticator{}
	}
	options := []Option{WithActionsSecurity(security, verifier)}
	if actions != nil {
		options = append(options, WithActions(actions))
	}
	return New(
		fakeStore{user: platform.User{ID: "user-1", Username: "alice", DisplayName: "Alice"}},
		actionsLore{branches: []loreclient.Branch{}},
		authenticator,
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		options...,
	)
}

func validFakeJobTokenVerifier() *fakeJobTokenVerifier {
	return &fakeJobTokenVerifier{verified: runner.VerifiedJobToken{Claims: runner.ActionsJobTokenClaims{
		JobID:         "3d691a42-9591-4e7c-a9db-a0cc44cb1685",
		RunID:         "c9430770-fd0b-41dd-a8ea-b84bbea1f487",
		Attempt:       1,
		RepositoryID:  "8d52f102-d46e-40b0-91ae-0b7997ef8760",
		PrincipalKind: "service",
		RESTScope:     runner.JobTokenRESTScope,
		GraphQLScope:  runner.JobTokenGraphQLScope,
	}}}
}

func sarifUploadJSON(t *testing.T, document []byte, compressed bool) string {
	t.Helper()
	payload := document
	if compressed {
		var output bytes.Buffer
		writer := gzip.NewWriter(&output)
		if _, err := writer.Write(document); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		payload = output.Bytes()
	}
	body, err := json.Marshal(sarifUploadRequest{
		CommitSHA: "revision-one",
		Ref:       "refs/heads/main",
		SARIF:     base64.StdEncoding.EncodeToString(payload),
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
