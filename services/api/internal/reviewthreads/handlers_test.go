package reviewthreads

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type fakeReviewStore struct {
	created   CreateInput
	replied   string
	pending   *PendingReview
	submitted SubmitInput
	discarded bool
	viewer    string
}

func (store *fakeReviewStore) List(_ context.Context, _ string, _ int64, viewer string) ([]Thread, error) {
	store.viewer = viewer
	return []Thread{}, nil
}

func (store *fakeReviewStore) Create(
	_ context.Context,
	actor platform.User,
	_ RepositoryRef,
	_ int64,
	input CreateInput,
) (Thread, error) {
	store.created = input
	return Thread{
		ID: uuid.NewString(), Path: input.Path, Side: input.Side,
		LineNumber: input.LineNumber, LineContent: input.LineContent,
		BaseRevision: input.ExpectedBaseRevision, HeadRevision: input.ExpectedHeadRevision,
		CreatedBy: actor.Username, Version: 1, Comments: []Comment{},
	}, nil
}

func (store *fakeReviewStore) Reply(
	_ context.Context,
	_ platform.User,
	_ RepositoryRef,
	_ int64,
	_ string,
	_ string,
	pendingReviewID string,
) (Comment, error) {
	store.replied = pendingReviewID
	return Comment{Pending: pendingReviewID != ""}, nil
}

func (store *fakeReviewStore) UpdateComment(
	context.Context, platform.User, RepositoryRef, int64, string, string, string, int,
) (Comment, error) {
	return Comment{}, nil
}

func (store *fakeReviewStore) DeleteComment(
	context.Context, platform.User, RepositoryRef, int64, string, string, int,
) error {
	return nil
}

func (store *fakeReviewStore) SetResolved(
	context.Context, platform.User, RepositoryRef, int64, string, bool, int,
) (Thread, error) {
	return Thread{}, nil
}

func (store *fakeReviewStore) PendingReview(
	_ context.Context, _ string, _ int64, author string,
) (*PendingReview, error) {
	if store.pending == nil || author != store.pending.Author {
		return nil, nil
	}
	return store.pending, nil
}

func (store *fakeReviewStore) StartPendingReview(
	_ context.Context, actor platform.User, _ RepositoryRef, _ int64,
) (PendingReview, bool, error) {
	if store.pending != nil {
		return *store.pending, false, nil
	}
	store.pending = &PendingReview{ID: uuid.NewString(), Author: actor.Username}
	return *store.pending, true, nil
}

func (store *fakeReviewStore) UpdatePendingReview(
	_ context.Context, actor platform.User, _ RepositoryRef, _ int64, body string,
) (PendingReview, error) {
	if store.pending == nil {
		return PendingReview{}, platform.ErrNotFound
	}
	store.pending.Body = body
	store.pending.Author = actor.Username
	return *store.pending, nil
}

func (store *fakeReviewStore) SubmitPendingReview(
	_ context.Context, _ platform.User, _ RepositoryRef, _ int64, input SubmitInput,
) (SubmitResult, error) {
	store.submitted = input
	return SubmitResult{Decision: input.Decision, PublishedComments: 2}, nil
}

func (store *fakeReviewStore) DiscardPendingReview(
	context.Context, platform.User, RepositoryRef, int64,
) error {
	store.discarded = true
	return nil
}

type fakeReviewRepositories struct{}

func (fakeReviewRepositories) LookupRepository(
	context.Context,
	*platform.User,
	string,
	string,
) (collab.Repository, error) {
	return collab.Repository{
		ID: uuid.NewString(), OrganizationID: uuid.NewString(), Owner: "acme", Slug: "game",
		LoreRepositoryID: "partition", LoreURL: "https://lore.invalid/partition", DefaultBranch: "main",
	}, nil
}

func (fakeReviewRepositories) RepositoryPermission(
	context.Context,
	platform.User,
	collab.Repository,
) (collab.Access, error) {
	return collab.Access{Permission: collab.PermWrite}, nil
}

func (fakeReviewRepositories) GetMergeRequest(
	context.Context,
	string,
	int64,
) (collab.MergeRequest, error) {
	return collab.MergeRequest{
		ID: uuid.NewString(), Number: 1, State: "open",
		SourceRevision: "head", TargetRevision: "base",
	}, nil
}

type fakeReviewActors struct {
	actor *platform.User
}

func (actors fakeReviewActors) ResolveActor(
	writer http.ResponseWriter,
	_ *http.Request,
) (platform.User, bool) {
	if actors.actor == nil {
		writeProblem(writer, http.StatusUnauthorized, "unauthorized", "Authentication is required")
		return platform.User{}, false
	}
	return *actors.actor, true
}

func (actors fakeReviewActors) ResolveOptionalActor(
	_ http.ResponseWriter,
	_ *http.Request,
) (*platform.User, bool) {
	return actors.actor, true
}

type fakeReviewCredentials struct{}

func (fakeReviewCredentials) ForRepository(
	context.Context,
	loreclient.CredentialRequest,
) (loreclient.Credential, error) {
	return loreclient.Credential{Partition: "partition", Scope: loreclient.ScopeRead}, nil
}

type fakeReviewCode struct {
	source string
	target string
	paths  []string
}

func (code *fakeReviewCode) RevisionDiff(
	_ context.Context,
	_ loreclient.RepositoryRef,
	source string,
	target string,
	paths []string,
	_ loreclient.Credential,
	_ int,
	_ int,
) (loreclient.Diff, error) {
	code.source, code.target, code.paths = source, target, paths
	return loreclient.Diff{
		Source: source, Target: target,
		Files: []loreclient.DiffFile{{Path: paths[0], Patch: "@@ -4 +4 @@\n-old\n+new\n"}},
	}, nil
}

func (*fakeReviewCode) Tree(
	context.Context, loreclient.RepositoryRef, string, string, loreclient.Credential, int,
) (loreclient.Tree, error) {
	return loreclient.Tree{}, nil
}

func (*fakeReviewCode) File(
	context.Context, loreclient.RepositoryRef, string, string, loreclient.Credential, int64,
) (loreclient.File, []byte, error) {
	return loreclient.File{}, nil, nil
}

func (*fakeReviewCode) RevisionHistory(
	context.Context, loreclient.RepositoryRef, string, string, loreclient.Credential, int,
) ([]loreclient.RevisionHistoryEntry, error) {
	return nil, nil
}

func (*fakeReviewCode) FileHistory(
	context.Context, loreclient.RepositoryRef, string, string, string, loreclient.Credential, int,
) ([]loreclient.FileHistoryEntry, error) {
	return nil, nil
}

func (*fakeReviewCode) RevisionInfo(
	context.Context, loreclient.RepositoryRef, string, loreclient.Credential,
) (loreclient.Revision, error) {
	return loreclient.Revision{}, nil
}

func TestCreateThreadValidatesTheCurrentLoreDiff(t *testing.T) {
	actor := platform.User{ID: uuid.NewString(), Username: "alice"}
	store := &fakeReviewStore{}
	code := &fakeReviewCode{}
	handler := reviewTestHandler(store, code, &actor)
	response := performReviewRequest(
		handler,
		http.MethodPost,
		"/api/v1/repositories/acme/game/merge-requests/1/review-threads",
		`{"path":"src/main.go","side":"right","lineNumber":4,"body":"Check this.",`+
			`"expectedBaseRevision":"base","expectedHeadRevision":"head"}`,
	)
	if response.Code != http.StatusCreated {
		t.Fatalf("create review thread = %d %s", response.Code, response.Body.String())
	}
	if code.source != "base" || code.target != "head" || len(code.paths) != 1 || code.paths[0] != "src/main.go" {
		t.Fatalf("Lore diff request = source %q, target %q, paths %v", code.source, code.target, code.paths)
	}
	if store.created.LineContent != "new" {
		t.Fatalf("stored line content = %q, want new", store.created.LineContent)
	}
}

func TestCreateThreadRequiresAuthenticationAndStrictJSON(t *testing.T) {
	store := &fakeReviewStore{}
	code := &fakeReviewCode{}
	path := "/api/v1/repositories/acme/game/merge-requests/1/review-threads"
	response := performReviewRequest(reviewTestHandler(store, code, nil), http.MethodPost, path, `{}`)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous create status = %d, want 401", response.Code)
	}
	actor := platform.User{ID: uuid.NewString(), Username: "alice"}
	response = performReviewRequest(
		reviewTestHandler(store, code, &actor), http.MethodPost, path,
		`{"path":"src/main.go","unknown":true}`,
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown JSON field status = %d, want 400", response.Code)
	}
}

func TestPendingReviewEndpointsBatchCommentsUntilSubmit(t *testing.T) {
	actor := platform.User{ID: uuid.NewString(), Username: "alice"}
	store := &fakeReviewStore{}
	handler := reviewTestHandler(store, &fakeReviewCode{}, &actor)
	base := "/api/v1/repositories/acme/game/merge-requests/1/reviews/pending"
	response := performReviewRequest(handler, http.MethodPost, base, `{}`)
	if response.Code != http.StatusCreated || store.pending == nil {
		t.Fatalf("start pending review = %d %s", response.Code, response.Body.String())
	}
	if response = performReviewRequest(handler, http.MethodPost, base, `{}`); response.Code != http.StatusOK {
		t.Fatalf("repeated start status = %d, want 200", response.Code)
	}
	response = performReviewRequest(
		handler, http.MethodPost,
		"/api/v1/repositories/acme/game/merge-requests/1/review-threads/"+uuid.NewString()+"/comments",
		`{"body":"Batched","pendingReviewId":"`+store.pending.ID+`"}`,
	)
	if response.Code != http.StatusCreated || store.replied != store.pending.ID {
		t.Fatalf("batched reply = %d, pending review = %q", response.Code, store.replied)
	}
	response = performReviewRequest(handler, http.MethodPatch, base, `{"body":"Looks good overall."}`)
	if response.Code != http.StatusOK || store.pending.Body != "Looks good overall." {
		t.Fatalf("update pending review = %d %s", response.Code, response.Body.String())
	}
	response = performReviewRequest(handler, http.MethodPost, base+"/submit", `{"verdict":"nope"}`)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid verdict status = %d, want 422", response.Code)
	}
	response = performReviewRequest(
		handler, http.MethodPost, base+"/submit", `{"verdict":"request_changes","body":"Please fix."}`,
	)
	if response.Code != http.StatusOK || store.submitted.Decision != "changes_requested" {
		t.Fatalf("submit = %d, decision = %q", response.Code, store.submitted.Decision)
	}
	if store.submitted.Body == nil || *store.submitted.Body != "Please fix." {
		t.Fatalf("submitted body = %v", store.submitted.Body)
	}
	if response = performReviewRequest(handler, http.MethodDelete, base, `{}`); response.Code != http.StatusNoContent {
		t.Fatalf("discard status = %d, want 204", response.Code)
	}
	if !store.discarded {
		t.Fatal("discard did not reach the store")
	}
}

func TestListThreadsReportsTheViewerPendingReview(t *testing.T) {
	actor := platform.User{ID: uuid.NewString(), Username: "alice"}
	store := &fakeReviewStore{pending: &PendingReview{ID: uuid.NewString(), Author: "alice", CommentCount: 3}}
	path := "/api/v1/repositories/acme/game/merge-requests/1/review-threads"
	response := performReviewRequest(reviewTestHandler(store, &fakeReviewCode{}, &actor), http.MethodGet, path, "")
	if response.Code != http.StatusOK || store.viewer != "alice" {
		t.Fatalf("list threads = %d, viewer = %q", response.Code, store.viewer)
	}
	if !strings.Contains(response.Body.String(), `"commentCount":3`) {
		t.Fatalf("list body = %s", response.Body.String())
	}
	anonymous := &fakeReviewStore{pending: store.pending}
	response = performReviewRequest(reviewTestHandler(anonymous, &fakeReviewCode{}, nil), http.MethodGet, path, "")
	if response.Code != http.StatusOK || anonymous.viewer != "" {
		t.Fatalf("anonymous list = %d, viewer = %q", response.Code, anonymous.viewer)
	}
	if strings.Contains(response.Body.String(), "pendingReview") {
		t.Fatalf("anonymous list body = %s", response.Body.String())
	}
}

func reviewTestHandler(store Store, code loreclient.CodeClient, actor *platform.User) http.Handler {
	mux := http.NewServeMux()
	Register(
		mux, store, fakeReviewRepositories{}, fakeReviewActors{actor: actor}, code,
		fakeReviewCredentials{}, slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	return mux
}

func performReviewRequest(handler http.Handler, method string, path string, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
