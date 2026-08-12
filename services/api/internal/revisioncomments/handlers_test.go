package revisioncomments

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const testRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type fakeStore struct {
	page        collab.RevisionCommentPage
	comment     collab.RevisionComment
	err         error
	createdBody string
	updatedBody string
	deletedID   string
}

func (store *fakeStore) ListRevisionComments(
	context.Context,
	*platform.User,
	collab.Repository,
	string,
	int,
	int,
) (collab.RevisionCommentPage, error) {
	return store.page, store.err
}

func (store *fakeStore) CreateRevisionComment(
	_ context.Context,
	_ platform.User,
	_ collab.Repository,
	_ string,
	body string,
) (collab.RevisionComment, error) {
	store.createdBody = body
	return store.comment, store.err
}

func (store *fakeStore) UpdateRevisionComment(
	_ context.Context,
	_ platform.User,
	_ collab.Repository,
	_ string,
	_ string,
	body string,
) (collab.RevisionComment, error) {
	store.updatedBody = body
	return store.comment, store.err
}

func (store *fakeStore) DeleteRevisionComment(
	_ context.Context,
	_ platform.User,
	_ collab.Repository,
	_ string,
	commentID string,
) error {
	store.deletedID = commentID
	return store.err
}

type fakeRepositories struct {
	repository collab.Repository
	err        error
}

func (repositories fakeRepositories) LookupRepository(
	context.Context,
	*platform.User,
	string,
	string,
) (collab.Repository, error) {
	return repositories.repository, repositories.err
}

type fakeActors struct {
	actor *platform.User
}

func (actors fakeActors) ResolveActor(
	writer http.ResponseWriter,
	_ *http.Request,
) (platform.User, bool) {
	if actors.actor == nil {
		writeProblem(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required")
		return platform.User{}, false
	}
	return *actors.actor, true
}

func (actors fakeActors) ResolveOptionalActor(
	_ http.ResponseWriter,
	_ *http.Request,
) (*platform.User, bool) {
	return actors.actor, true
}

type fakeCredentials struct {
	request loreclient.CredentialRequest
	err     error
}

func (credentials *fakeCredentials) ForRepository(
	_ context.Context,
	request loreclient.CredentialRequest,
) (loreclient.Credential, error) {
	credentials.request = request
	return loreclient.Credential{}, credentials.err
}

type fakeCode struct {
	revision loreclient.Revision
	err      error
}

func (code fakeCode) Tree(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	loreclient.Credential,
	int,
) (loreclient.Tree, error) {
	return loreclient.Tree{}, errors.New("unexpected Tree call")
}

func (code fakeCode) File(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	loreclient.Credential,
	int64,
) (loreclient.File, []byte, error) {
	return loreclient.File{}, nil, errors.New("unexpected File call")
}

func (code fakeCode) RevisionHistory(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	loreclient.Credential,
	int,
) ([]loreclient.RevisionHistoryEntry, error) {
	return nil, errors.New("unexpected RevisionHistory call")
}

func (code fakeCode) FileHistory(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	string,
	loreclient.Credential,
	int,
) ([]loreclient.FileHistoryEntry, error) {
	return nil, errors.New("unexpected FileHistory call")
}

func (code fakeCode) RevisionInfo(
	context.Context,
	loreclient.RepositoryRef,
	string,
	loreclient.Credential,
) (loreclient.Revision, error) {
	return code.revision, code.err
}

func (code fakeCode) RevisionDiff(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	[]string,
	loreclient.Credential,
	int,
	int,
) (loreclient.Diff, error) {
	return loreclient.Diff{}, errors.New("unexpected RevisionDiff call")
}

func testRepository() collab.Repository {
	return collab.Repository{
		ID:             "11111111-1111-4111-8111-111111111111",
		OrganizationID: "22222222-2222-4222-8222-222222222222",
		Owner:          "acme", Slug: "game", LoreRepositoryID: strings.Repeat("b", 32),
		LoreURL: "lore://localhost/" + strings.Repeat("b", 32), DefaultBranch: "main",
	}
}

func testActor() platform.User {
	return platform.User{
		ID: "33333333-3333-4333-8333-333333333333", Username: "alice", DisplayName: "Alice",
	}
}

func testComment() collab.RevisionComment {
	return collab.RevisionComment{
		ID: "44444444-4444-4444-8444-444444444444", Revision: testRevision,
		Author: collab.RevisionCommentAuthor{ID: testActor().ID, Username: "alice"},
		Body:   "Looks good", CreatedAt: time.Now().UTC(), ViewerCanUpdate: true,
	}
}

func testAPI(
	store collab.RevisionCommentStore,
	actors collab.ActorResolver,
	code loreclient.CodeClient,
	credentials loreclient.CredentialProvider,
) *API {
	return NewAPI(
		store, fakeRepositories{repository: testRepository()}, actors, code, credentials,
		"public-reader-subject", slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

func request(method string, path string, body string) *http.Request {
	value := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		value.Header.Set("Content-Type", "application/json")
	}
	value.SetPathValue("owner", "acme")
	value.SetPathValue("repository", "game")
	value.SetPathValue("revision", testRevision)
	value.SetPathValue("commentID", testComment().ID)
	return value
}

func TestListAnonymousCommentsUsesPublicReaderCredential(t *testing.T) {
	store := &fakeStore{page: collab.RevisionCommentPage{Items: []collab.RevisionComment{testComment()}}}
	credentials := &fakeCredentials{}
	api := testAPI(
		store, fakeActors{}, fakeCode{revision: loreclient.Revision{Revision: testRevision}}, credentials,
	)
	writer := httptest.NewRecorder()
	api.list(writer, request(http.MethodGet, "/?page=1&perPage=30", ""))
	if writer.Code != http.StatusOK || !strings.Contains(writer.Body.String(), `"Looks good"`) {
		t.Fatalf("status = %d, body = %s", writer.Code, writer.Body.String())
	}
	principal := credentials.request.Principal
	if principal.ServicePurpose != loreclient.ServicePurposePublicReader ||
		principal.Subject != "public-reader-subject" {
		t.Fatalf("credential principal = %#v", principal)
	}
}

func TestCreateCommentVerifiesLoreRevision(t *testing.T) {
	actor := testActor()
	store := &fakeStore{comment: testComment()}
	credentials := &fakeCredentials{}
	api := testAPI(
		store, fakeActors{actor: &actor},
		fakeCode{revision: loreclient.Revision{Revision: testRevision}}, credentials,
	)
	writer := httptest.NewRecorder()
	api.create(writer, request(http.MethodPost, "/", `{"body":"Looks good"}`))
	if writer.Code != http.StatusCreated || store.createdBody != "Looks good" {
		t.Fatalf("status = %d, body = %s, stored = %q", writer.Code, writer.Body.String(), store.createdBody)
	}
	if credentials.request.Principal.UserID != actor.ID || credentials.request.Scope != loreclient.ScopeRead {
		t.Fatalf("credential request = %#v", credentials.request)
	}
}

func TestCreateCommentRejectsMissingRevisionAndUnknownField(t *testing.T) {
	actor := testActor()
	tests := []struct {
		name string
		code fakeCode
		body string
		want int
	}{
		{name: "missing revision", code: fakeCode{err: loreclient.ErrNotFound}, body: `{"body":"x"}`,
			want: http.StatusNotFound},
		{name: "unknown field", code: fakeCode{}, body: `{"body":"x","extra":true}`,
			want: http.StatusBadRequest},
		{name: "control character", code: fakeCode{}, body: `{"body":"x\u0000y"}`,
			want: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api := testAPI(&fakeStore{}, fakeActors{actor: &actor}, test.code, &fakeCredentials{})
			writer := httptest.NewRecorder()
			api.create(writer, request(http.MethodPost, "/", test.body))
			if writer.Code != test.want {
				t.Fatalf("status = %d, body = %s", writer.Code, writer.Body.String())
			}
		})
	}
}

func TestUpdateAndDeleteComment(t *testing.T) {
	actor := testActor()
	store := &fakeStore{comment: testComment()}
	api := testAPI(store, fakeActors{actor: &actor}, nil, nil)
	updateWriter := httptest.NewRecorder()
	api.update(updateWriter, request(http.MethodPatch, "/", `{"body":"Updated"}`))
	if updateWriter.Code != http.StatusOK || store.updatedBody != "Updated" {
		t.Fatalf("status = %d, body = %s", updateWriter.Code, updateWriter.Body.String())
	}
	deleteWriter := httptest.NewRecorder()
	api.delete(deleteWriter, request(http.MethodDelete, "/", ""))
	if deleteWriter.Code != http.StatusNoContent || store.deletedID != testComment().ID {
		t.Fatalf("status = %d, deleted = %q", deleteWriter.Code, store.deletedID)
	}
}

func TestListRejectsUnknownAndDuplicateQueryFields(t *testing.T) {
	api := testAPI(&fakeStore{}, fakeActors{}, fakeCode{}, &fakeCredentials{})
	for _, query := range []string{"?unknown=1", "?page=1&page=2", "?page=", "?perPage=101"} {
		writer := httptest.NewRecorder()
		api.list(writer, request(http.MethodGet, "/"+query, ""))
		if writer.Code != http.StatusBadRequest {
			t.Fatalf("query = %q, status = %d", query, writer.Code)
		}
	}
}

func TestCreateRejectsArchivedRepository(t *testing.T) {
	actor := testActor()
	repository := testRepository()
	archivedAt := time.Now().UTC()
	repository.ArchivedAt = &archivedAt
	api := NewAPI(
		&fakeStore{}, fakeRepositories{repository: repository}, fakeActors{actor: &actor},
		fakeCode{}, &fakeCredentials{}, "public-reader-subject", nil,
	)
	writer := httptest.NewRecorder()
	api.create(writer, request(http.MethodPost, "/", `{"body":"x"}`))
	if writer.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", writer.Code, writer.Body.String())
	}
}

func TestRoutesAreRegistered(t *testing.T) {
	actor := testActor()
	store := &fakeStore{comment: testComment()}
	mux := http.NewServeMux()
	Register(
		mux, store, fakeRepositories{repository: testRepository()}, fakeActors{actor: &actor},
		fakeCode{}, &fakeCredentials{}, "public-reader-subject", nil,
	)
	writer := httptest.NewRecorder()
	mux.ServeHTTP(writer, request(
		http.MethodDelete,
		"/api/v1/repositories/acme/game/revisions/"+testRevision+"/comments/"+testComment().ID,
		"",
	))
	if writer.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", writer.Code, writer.Body.String())
	}
}
