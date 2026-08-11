package collab

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

// testAuthenticator maps "Bearer <subject>" to a principal with that subject.
type testAuthenticator struct{}

func (testAuthenticator) Authenticate(_ context.Context, authorization string) (auth.Principal, error) {
	scheme, token, found := strings.Cut(authorization, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return auth.Principal{}, auth.ErrMissingToken
	}
	return auth.Principal{Issuer: "test", Subject: strings.TrimSpace(token), Username: token}, nil
}

type testActorResolver struct {
	store Store
}

func (resolver testActorResolver) ResolveActor(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, bool) {
	principal, err := (testAuthenticator{}).Authenticate(
		request.Context(), request.Header.Get("Authorization"),
	)
	if err != nil {
		writeProblem(writer, http.StatusUnauthorized, "authentication_required",
			"Authentication is required")
		return platform.User{}, false
	}
	user, err := resolver.store.EnsureUser(request.Context(), principal)
	if err != nil {
		writeProblem(writer, http.StatusInternalServerError, "internal_error",
			"The request could not be completed")
		return platform.User{}, false
	}
	return user, true
}

func (resolver testActorResolver) ResolveOptionalActor(
	writer http.ResponseWriter,
	request *http.Request,
) (*platform.User, bool) {
	if strings.TrimSpace(request.Header.Get("Authorization")) == "" {
		return nil, true
	}
	user, ok := resolver.ResolveActor(writer, request)
	if !ok {
		return nil, false
	}
	return &user, true
}

// fakeStore is a configurable Store implementation for handler tests. Each
// operation delegates to a function field so individual tests can inject
// success, validation, auth and not-found behavior without a database.
type fakeStore struct {
	user          platform.User
	ensureUserErr error
	lookupRepo    func(actor *platform.User, owner, slug string) (Repository, error)
	permission    func(actor platform.User, repo Repository) (Access, error)
	getIssue      func(repoID string, number int64) (Issue, error)
	updateIssue   func(actor platform.User, repoID string, number int64, input UpdateIssueInput) (Issue, error)
	listComments  func(repoID string, number int64, page Page) (Result[IssueComment], error)
	createComment func(actor platform.User, repoID string, number int64, body string) (IssueComment, error)
	updateComment func(
		actor platform.User, repoID string, number int64, commentID, body string,
	) (IssueComment, error)
	deleteComment func(actor platform.User, repoID string, number int64, commentID string) error
	listLabels    func(repoID string, page Page) (Result[Label], error)
	createLabel   func(actor platform.User, repoID string, input LabelInput) (Label, error)
	updateLabel   func(actor platform.User, repoID, labelID string, input LabelInput) (Label, error)
	deleteLabel   func(actor platform.User, repoID, labelID string) error
	applyLabel    func(actor platform.User, repoID string, number int64, labelID string) (Label, bool, error)
	removeLabel   func(actor platform.User, repoID string, number int64, labelID string) error
	getMR         func(repoID string, number int64) (MergeRequest, error)
	updateMR      func(actor platform.User, repoID string, number int64,
		input UpdateMergeRequestInput) (MergeRequest, error)
	listReviews  func(repoID string, number int64) (ReviewSummary, error)
	createReview func(actor platform.User, repoID string, number int64, input ReviewInput) (Review, error)
	listRules    func(repoID string) ([]BranchRule, error)
	createRule   func(actor platform.User, repoID string, input BranchRuleInput) (BranchRule, error)
	updateRule   func(actor platform.User, repoID, ruleID string, input BranchRuleInput) (BranchRule, error)
	deleteRule   func(actor platform.User, repoID, ruleID string) error
}

func (s *fakeStore) EnsureUser(context.Context, auth.Principal) (platform.User, error) {
	if s.ensureUserErr != nil {
		return platform.User{}, s.ensureUserErr
	}
	return s.user, nil
}

func (s *fakeStore) LookupRepository(_ context.Context, actor *platform.User, owner, slug string) (Repository, error) {
	if s.lookupRepo != nil {
		return s.lookupRepo(actor, owner, slug)
	}
	return Repository{}, platform.ErrNotFound
}

func (s *fakeStore) RepositoryPermission(_ context.Context, actor platform.User, repo Repository) (Access, error) {
	if s.permission != nil {
		return s.permission(actor, repo)
	}
	return Access{Permission: PermNone}, nil
}

func (s *fakeStore) GetIssue(_ context.Context, repoID string, number int64) (Issue, error) {
	if s.getIssue != nil {
		return s.getIssue(repoID, number)
	}
	return Issue{}, platform.ErrNotFound
}

func (s *fakeStore) UpdateIssue(_ context.Context, actor platform.User, repoID string, number int64,
	input UpdateIssueInput,
) (Issue, error) {
	if s.updateIssue != nil {
		return s.updateIssue(actor, repoID, number, input)
	}
	return Issue{}, platform.ErrNotFound
}

func (s *fakeStore) ListIssueComments(_ context.Context, repoID string, number int64,
	page Page,
) (Result[IssueComment], error) {
	if s.listComments != nil {
		return s.listComments(repoID, number, page)
	}
	return Result[IssueComment]{}, nil
}

func (s *fakeStore) CreateIssueComment(_ context.Context, actor platform.User, repoID string,
	number int64, body string,
) (IssueComment, error) {
	if s.createComment != nil {
		return s.createComment(actor, repoID, number, body)
	}
	return IssueComment{}, platform.ErrNotFound
}

func (s *fakeStore) UpdateIssueComment(_ context.Context, actor platform.User,
	repoID string, number int64, commentID, body string,
) (IssueComment, error) {
	if s.updateComment != nil {
		return s.updateComment(actor, repoID, number, commentID, body)
	}
	return IssueComment{}, platform.ErrNotFound
}

func (s *fakeStore) DeleteIssueComment(
	_ context.Context, actor platform.User, repoID string, number int64, commentID string,
) error {
	if s.deleteComment != nil {
		return s.deleteComment(actor, repoID, number, commentID)
	}
	return platform.ErrNotFound
}

func (s *fakeStore) ListLabels(_ context.Context, repoID string, page Page) (Result[Label], error) {
	if s.listLabels != nil {
		return s.listLabels(repoID, page)
	}
	return Result[Label]{}, nil
}

func (s *fakeStore) CreateLabel(_ context.Context, actor platform.User, repoID string,
	input LabelInput,
) (Label, error) {
	if s.createLabel != nil {
		return s.createLabel(actor, repoID, input)
	}
	return Label{}, platform.ErrNotFound
}

func (s *fakeStore) UpdateLabel(_ context.Context, actor platform.User, repoID, labelID string,
	input LabelInput,
) (Label, error) {
	if s.updateLabel != nil {
		return s.updateLabel(actor, repoID, labelID, input)
	}
	return Label{}, platform.ErrNotFound
}

func (s *fakeStore) DeleteLabel(_ context.Context, actor platform.User, repoID, labelID string) error {
	if s.deleteLabel != nil {
		return s.deleteLabel(actor, repoID, labelID)
	}
	return platform.ErrNotFound
}

func (s *fakeStore) ApplyLabel(_ context.Context, actor platform.User, repoID string,
	number int64, labelID string,
) (Label, bool, error) {
	if s.applyLabel != nil {
		return s.applyLabel(actor, repoID, number, labelID)
	}
	return Label{}, false, platform.ErrNotFound
}

func (s *fakeStore) RemoveLabel(_ context.Context, actor platform.User, repoID string,
	number int64, labelID string,
) error {
	if s.removeLabel != nil {
		return s.removeLabel(actor, repoID, number, labelID)
	}
	return platform.ErrNotFound
}

func (s *fakeStore) GetMergeRequest(_ context.Context, repoID string, number int64) (MergeRequest, error) {
	if s.getMR != nil {
		return s.getMR(repoID, number)
	}
	return MergeRequest{}, platform.ErrNotFound
}

func (s *fakeStore) UpdateMergeRequest(_ context.Context, actor platform.User, repoID string,
	number int64, input UpdateMergeRequestInput,
) (MergeRequest, error) {
	if s.updateMR != nil {
		return s.updateMR(actor, repoID, number, input)
	}
	return MergeRequest{}, platform.ErrNotFound
}

func (s *fakeStore) ListReviews(_ context.Context, repoID string, number int64) (ReviewSummary, error) {
	if s.listReviews != nil {
		return s.listReviews(repoID, number)
	}
	return ReviewSummary{}, nil
}

func (s *fakeStore) CreateReview(_ context.Context, actor platform.User, repoID string,
	number int64, input ReviewInput,
) (Review, bool, error) {
	if s.createReview != nil {
		review, err := s.createReview(actor, repoID, number, input)
		return review, true, err
	}
	return Review{}, false, platform.ErrNotFound
}

func (s *fakeStore) ListBranchRules(_ context.Context, repoID string) ([]BranchRule, error) {
	if s.listRules != nil {
		return s.listRules(repoID)
	}
	return nil, nil
}

func (s *fakeStore) CreateBranchRule(_ context.Context, actor platform.User, repoID string,
	input BranchRuleInput,
) (BranchRule, error) {
	if s.createRule != nil {
		return s.createRule(actor, repoID, input)
	}
	return BranchRule{}, platform.ErrNotFound
}

func (s *fakeStore) UpdateBranchRule(_ context.Context, actor platform.User, repoID, ruleID string,
	input BranchRuleInput,
) (BranchRule, error) {
	if s.updateRule != nil {
		return s.updateRule(actor, repoID, ruleID, input)
	}
	return BranchRule{}, platform.ErrNotFound
}

func (s *fakeStore) DeleteBranchRule(
	_ context.Context, actor platform.User, repoID, ruleID string,
) error {
	if s.deleteRule != nil {
		return s.deleteRule(actor, repoID, ruleID)
	}
	return platform.ErrNotFound
}

func newTestAPI(store Store) http.Handler {
	mux := http.NewServeMux()
	Register(mux, store, testActorResolver{store: store}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return mux
}

func doRequest(handler http.Handler, method, target, body string, headers ...string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	}
	request := httptest.NewRequest(method, target, reader)
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func errorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error body: %v (body=%q)", err, recorder.Body.String())
	}
	return envelope.Error.Code
}

func alice() platform.User {
	return platform.User{ID: "alice-id", Username: "alice"}
}

func repoFor(owner, slug string) Repository {
	return Repository{ID: "repo-1", OrganizationID: "org-1", Owner: owner, Slug: slug, Visibility: "public"}
}

func TestGetIssuePublic(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		getIssue: func(_ string, _ int64) (Issue, error) {
			return Issue{ID: "issue-1", Number: 3, Title: "Bug", State: "open", Author: "alice"}, nil
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodGet,
		"/api/v1/repositories/acme/lore/issues/3", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	var issue Issue
	if err := json.Unmarshal(recorder.Body.Bytes(), &issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	if issue.Number != 3 || issue.Title != "Bug" {
		t.Fatalf("unexpected issue %+v", issue)
	}
}

func TestGetIssueNotFound(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		getIssue: func(string, int64) (Issue, error) { return Issue{}, platform.ErrNotFound },
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodGet,
		"/api/v1/repositories/acme/lore/issues/99", "")
	if recorder.Code != http.StatusNotFound || errorCode(t, recorder) != "not_found" {
		t.Fatalf("expected 404 not_found, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestGetIssuePrivateLeaksNoExistence(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return Repository{}, platform.ErrNotFound
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodGet,
		"/api/v1/repositories/acme/secret/issues/1", "")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for private repo, got %d", recorder.Code)
	}
}

func TestPatchIssueEmptyBody(t *testing.T) {
	t.Parallel()
	store := &fakeStore{user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodPatch,
		"/api/v1/repositories/acme/lore/issues/3", `{}`,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusBadRequest || errorCode(t, recorder) != "invalid_input" {
		t.Fatalf("expected 400 invalid_input, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestPatchIssueUnauthenticated(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodPatch,
		"/api/v1/repositories/acme/lore/issues/3", `{"title":"x"}`,
		"Content-Type", "application/json")
	if recorder.Code != http.StatusUnauthorized || errorCode(t, recorder) != "authentication_required" {
		t.Fatalf("expected 401, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestPatchIssuePreconditionFailed(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		updateIssue: func(platform.User, string, int64, UpdateIssueInput) (Issue, error) {
			return Issue{}, ErrPreconditionFailed
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodPatch,
		"/api/v1/repositories/acme/lore/issues/3", `{"title":"x"}`,
		"Authorization", "Bearer alice", "Content-Type", "application/json",
		"If-Match", `"2024-01-01T00:00:00Z"`)
	if recorder.Code != http.StatusPreconditionFailed || errorCode(t, recorder) != "precondition_failed" {
		t.Fatalf("expected 412 precondition_failed, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateCommentSuccess(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		createComment: func(_ platform.User, _ string, _ int64, body string) (IssueComment, error) {
			return IssueComment{ID: "comment-1", Body: body, Author: "alice"}, nil
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodPost,
		"/api/v1/repositories/acme/lore/issues/3/comments", `{"body":"looks good"}`,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d %s", recorder.Code, recorder.Body.String())
	}
	if loc := recorder.Header().Get("Location"); !strings.HasSuffix(loc, "/comment-1") {
		t.Fatalf("unexpected Location %q", loc)
	}
	var comment IssueComment
	if err := json.Unmarshal(recorder.Body.Bytes(), &comment); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	if !comment.ViewerCanUpdate {
		t.Fatal("created comment did not return viewerCanUpdate")
	}
}

func TestCreateCommentBlankBody(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodPost,
		"/api/v1/repositories/acme/lore/issues/3/comments", `{"body":"   "}`,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusBadRequest || errorCode(t, recorder) != "invalid_input" {
		t.Fatalf("expected 400 invalid_input, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestDeleteCommentNoContent(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		deleteComment: func(platform.User, string, int64, string) error { return nil },
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodDelete,
		"/api/v1/repositories/acme/lore/issues/3/comments/comment-1", "",
		"Authorization", "Bearer alice")
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", recorder.Code)
	}
}

func TestCommentMutationUsesIssueRouteScope(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		updateComment: func(_ platform.User, repoID string, number int64, commentID, body string) (IssueComment, error) {
			if repoID != "repo-1" || number != 9 || commentID != "comment-1" {
				t.Fatalf("store received wrong comment scope: repo=%q number=%d id=%q", repoID, number, commentID)
			}
			return IssueComment{}, platform.ErrNotFound
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodPatch,
		"/api/v1/repositories/acme/lore/issues/9/comments/comment-1", `{"body":"edited"}`,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusNotFound || errorCode(t, recorder) != "not_found" {
		t.Fatalf("cross-issue comment mutation: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestLabelMutationUsesRepositoryRouteScope(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		updateLabel: func(_ platform.User, repoID, labelID string, input LabelInput) (Label, error) {
			if repoID != "repo-1" || labelID != "label-1" {
				t.Fatalf("store received wrong label scope: repo=%q id=%q", repoID, labelID)
			}
			return Label{}, platform.ErrNotFound
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodPatch,
		"/api/v1/repositories/acme/lore/labels/label-1", `{"name":"bug","color":"ff0000"}`,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusNotFound || errorCode(t, recorder) != "not_found" {
		t.Fatalf("cross-repository label mutation: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestBranchRuleMutationUsesRepositoryRouteScope(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		updateRule: func(_ platform.User, repoID, ruleID string, input BranchRuleInput) (BranchRule, error) {
			if repoID != "repo-1" || ruleID != "rule-1" {
				t.Fatalf("store received wrong branch-rule scope: repo=%q id=%q", repoID, ruleID)
			}
			return BranchRule{}, platform.ErrNotFound
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodPatch,
		"/api/v1/repositories/acme/lore/branch-rules/rule-1",
		`{"pattern":"main","requiredApprovals":1}`,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusNotFound || errorCode(t, recorder) != "not_found" {
		t.Fatalf("cross-repository branch-rule mutation: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateLabelForbiddenForRead(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermRead}, nil
		},
	}
	handler := newTestAPI(store)
	body := `{"name":"bug","color":"ff0000"}`
	recorder := doRequest(handler, http.MethodPost,
		"/api/v1/repositories/acme/lore/labels", body,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusForbidden || errorCode(t, recorder) != "forbidden" {
		t.Fatalf("expected 403 forbidden, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateLabelSuccess(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermWrite}, nil
		},
		createLabel: func(_ platform.User, _ string, input LabelInput) (Label, error) {
			return Label{ID: "label-1", Name: input.Name, Color: input.Color}, nil
		},
	}
	handler := newTestAPI(store)
	body := `{"name":"bug","color":"ff0000"}`
	recorder := doRequest(handler, http.MethodPost,
		"/api/v1/repositories/acme/lore/labels", body,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateLabelConflict(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermWrite}, nil
		},
		createLabel: func(platform.User, string, LabelInput) (Label, error) {
			return Label{}, platform.ErrConflict
		},
	}
	handler := newTestAPI(store)
	body := `{"name":"bug","color":"ff0000"}`
	recorder := doRequest(handler, http.MethodPost,
		"/api/v1/repositories/acme/lore/labels", body,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusConflict || errorCode(t, recorder) != "conflict" {
		t.Fatalf("expected 409 conflict, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateLabelInvalidColor(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermWrite}, nil
		},
	}
	handler := newTestAPI(store)
	body := `{"name":"bug","color":"xyz"}`
	recorder := doRequest(handler, http.MethodPost,
		"/api/v1/repositories/acme/lore/labels", body,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusBadRequest || errorCode(t, recorder) != "invalid_input" {
		t.Fatalf("expected 400 invalid_input, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestApplyLabelIdempotent(t *testing.T) {
	t.Parallel()
	calls := 0
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermTriage}, nil
		},
		applyLabel: func(platform.User, string, int64, string) (Label, bool, error) {
			calls++
			return Label{ID: "label-1", Name: "bug"}, calls == 1, nil
		},
	}
	handler := newTestAPI(store)
	first := doRequest(handler, http.MethodPut,
		"/api/v1/repositories/acme/lore/issues/3/labels/label-1", "",
		"Authorization", "Bearer alice")
	if first.Code != http.StatusCreated {
		t.Fatalf("first apply expected 201, got %d", first.Code)
	}
	second := doRequest(handler, http.MethodPut,
		"/api/v1/repositories/acme/lore/issues/3/labels/label-1", "",
		"Authorization", "Bearer alice")
	if second.Code != http.StatusOK {
		t.Fatalf("duplicate apply expected 200, got %d", second.Code)
	}
}

func TestApplyLabelForbiddenForRead(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermRead}, nil
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodPut,
		"/api/v1/repositories/acme/lore/issues/3/labels/label-1", "",
		"Authorization", "Bearer alice")
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
}

func TestRemoveLabelNoContent(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermTriage}, nil
		},
		removeLabel: func(platform.User, string, int64, string) error { return nil },
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodDelete,
		"/api/v1/repositories/acme/lore/issues/3/labels/label-1", "",
		"Authorization", "Bearer alice")
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", recorder.Code)
	}
}

func TestCreateReviewCannotReviewOwn(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		createReview: func(platform.User, string, int64, ReviewInput) (Review, error) {
			return Review{}, ErrCannotReviewOwn
		},
	}
	handler := newTestAPI(store)
	body := `{"decision":"approved"}`
	recorder := doRequest(handler, http.MethodPost,
		"/api/v1/repositories/acme/lore/merge-requests/3/reviews", body,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusUnprocessableEntity || errorCode(t, recorder) != "cannot_review_own" {
		t.Fatalf("expected 422 cannot_review_own, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateReviewInvalidDecision(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
	}
	handler := newTestAPI(store)
	body := `{"decision":"rejected"}`
	recorder := doRequest(handler, http.MethodPost,
		"/api/v1/repositories/acme/lore/merge-requests/3/reviews", body,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusBadRequest || errorCode(t, recorder) != "invalid_input" {
		t.Fatalf("expected 400 invalid_input, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestListReviewsAggregate(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		listReviews: func(string, int64) (ReviewSummary, error) {
			return ReviewSummary{
				CurrentRevision: "rev-1",
				CurrentReviews:  []Review{{Decision: "approved"}, {Decision: "approved"}, {Decision: "changes_requested"}},
				Approvals:       2,
				ChangeRequests:  1,
			}, nil
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodGet,
		"/api/v1/repositories/acme/lore/merge-requests/3/reviews", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	var summary ReviewSummary
	if err := json.Unmarshal(recorder.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.Approvals != 2 || summary.ChangeRequests != 1 {
		t.Fatalf("unexpected aggregate %+v", summary)
	}
}

func TestCreateBranchRuleAdminSuccess(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermAdmin}, nil
		},
		createRule: func(_ platform.User, _ string, input BranchRuleInput) (BranchRule, error) {
			return BranchRule{ID: "rule-1", Pattern: input.Pattern, RequiredApprovals: input.RequiredApprovals}, nil
		},
	}
	handler := newTestAPI(store)
	body := `{"pattern":"main","requiredApprovals":2}`
	recorder := doRequest(handler, http.MethodPost,
		"/api/v1/repositories/acme/lore/branch-rules", body,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateBranchRuleForbiddenForWrite(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermWrite}, nil
		},
	}
	handler := newTestAPI(store)
	body := `{"pattern":"main","requiredApprovals":2}`
	recorder := doRequest(handler, http.MethodPost,
		"/api/v1/repositories/acme/lore/branch-rules", body,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusForbidden || errorCode(t, recorder) != "forbidden" {
		t.Fatalf("expected 403 forbidden, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateBranchRuleOrgMaintainerDenied(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermWrite, OrgMaintainer: true}, nil
		},
		createRule: func(platform.User, string, BranchRuleInput) (BranchRule, error) {
			return BranchRule{ID: "rule-1"}, nil
		},
	}
	handler := newTestAPI(store)
	body := `{"pattern":"main","requiredApprovals":0}`
	recorder := doRequest(handler, http.MethodPost,
		"/api/v1/repositories/acme/lore/branch-rules", body,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("org maintainer expected 403, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateBranchRuleInvalidApprovals(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermAdmin}, nil
		},
	}
	handler := newTestAPI(store)
	body := `{"pattern":"main","requiredApprovals":200}`
	recorder := doRequest(handler, http.MethodPost,
		"/api/v1/repositories/acme/lore/branch-rules", body,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusBadRequest || errorCode(t, recorder) != "invalid_input" {
		t.Fatalf("expected 400 invalid_input, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestListBranchRulesReadable(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		listRules: func(string) ([]BranchRule, error) {
			return []BranchRule{{ID: "rule-1", Pattern: "main"}}, nil
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodGet,
		"/api/v1/repositories/acme/lore/branch-rules", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
}

func TestMalformedNumberReturns404(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodGet,
		"/api/v1/repositories/acme/lore/issues/abc", "")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-numeric number, got %d", recorder.Code)
	}
}

func TestInvalidJSONReturns400(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodPost,
		"/api/v1/repositories/acme/lore/issues/3/comments", `{not json`,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusBadRequest || errorCode(t, recorder) != "invalid_json" {
		t.Fatalf("expected 400 invalid_json, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestJSONMutationRequiresJSONContentType(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodPost,
		"/api/v1/repositories/acme/lore/issues/3/comments", `{"body":"comment"}`,
		"Authorization", "Bearer alice", "Content-Type", "text/plain")
	if recorder.Code != http.StatusUnsupportedMediaType || errorCode(t, recorder) != "unsupported_media_type" {
		t.Fatalf("expected 415 unsupported_media_type, got %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestJSONContentType(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		getIssue: func(string, int64) (Issue, error) {
			return Issue{ID: "i", Number: 1, Title: "t", State: "open", Author: "a"}, nil
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodGet,
		"/api/v1/repositories/acme/lore/issues/1", "")
	if recorder.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("expected json content type, got %q", recorder.Header().Get("Content-Type"))
	}
}
