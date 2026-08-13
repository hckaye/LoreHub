package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestParseCodeSearchQuery(t *testing.T) {
	query, err := parseCodeSearchQuery("repo:Acme/lore-hub Needle NEEDLE")
	if err != nil {
		t.Fatal(err)
	}
	if query.Owner != "Acme" || query.Repository != "lore-hub" || len(query.Terms) != 1 || query.Terms[0] != "needle" {
		t.Fatalf("parsed code search query = %#v", query)
	}

	for _, test := range []struct {
		name  string
		value string
		want  error
	}{
		{name: "missing repository", value: "needle", want: errCodeSearchRepositoryRequired},
		{name: "invalid repository", value: "repo:acme/lore/hub needle", want: errCodeSearchRepositoryInvalid},
		{name: "missing term", value: "repo:acme/lore", want: errCodeSearchTermRequired},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseCodeSearchQuery(test.value)
			if !errors.Is(err, test.want) {
				t.Fatalf("parseCodeSearchQuery(%q) error = %v, want %v", test.value, err, test.want)
			}
		})
	}
}

func TestCodeSearchMatchesCaseInsensitiveAndCountsOccurrences(t *testing.T) {
	matches, count := codeSearchMatches("  Needle needle  \nno match\nNEEDLE", []string{"needle"})
	if count != 3 || len(matches) != 2 {
		t.Fatalf("matches = %#v, count = %d", matches, count)
	}
	if matches[0].LineNumber != 1 || matches[0].Snippet != "Needle needle" || matches[1].LineNumber != 3 {
		t.Fatalf("unexpected matches = %#v", matches)
	}
}

func TestScanCodeSearchWalksTreesSkipsBinaryAndHonorsCaps(t *testing.T) {
	client := &codeSearchFakeClient{
		trees: map[string]loreclient.Tree{
			"": {Entries: []loreclient.TreeEntry{
				{Name: "README.md", Path: "README.md", Kind: "file", Size: 22},
				{Name: "image.png", Path: "image.png", Kind: "file", Size: 4},
				{Name: "src", Path: "src", Kind: "directory"},
				{Name: "too-large.txt", Path: "too-large.txt", Kind: "file", Size: uint64(codeSearchMaxFileBytes + 1)},
			}},
			"src": {Entries: []loreclient.TreeEntry{
				{Name: "main.go", Path: "src/main.go", Kind: "file", Size: 19},
			}},
		},
		files: map[string]codeSearchFakeFile{
			"README.md":   {body: []byte("Needle in readme\n"), size: 17},
			"image.png":   {body: []byte{0, 1, 2, 3}, binary: true, size: 4},
			"src/main.go": {body: []byte("return NEEDLE\n"), size: 14},
		},
	}
	result, err := scanCodeSearch(
		context.Background(), client, loreclient.RepositoryRef{CacheKey: "repo"}, "revision", loreclient.Credential{},
		[]string{"needle"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Truncated != true || len(result.Files) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if result.Files[0].Path != "README.md" || result.Files[1].Path != "src/main.go" {
		t.Fatalf("file order = %#v", result.Files)
	}
	if len(client.fileCalls) != 3 {
		t.Fatalf("file calls = %#v", client.fileCalls)
	}
}

func TestScanCodeSearchStopsAtTotalByteCap(t *testing.T) {
	body := make([]byte, codeSearchMaxFileBytes)
	client := &codeSearchFakeClient{
		trees: map[string]loreclient.Tree{"": {Entries: makeCodeSearchFiles(65, uint64(codeSearchMaxFileBytes))}},
		files: map[string]codeSearchFakeFile{},
	}
	for _, entry := range client.trees[""].Entries {
		client.files[entry.Path] = codeSearchFakeFile{body: body, size: uint64(len(body))}
	}
	result, err := scanCodeSearch(
		context.Background(), client, loreclient.RepositoryRef{CacheKey: "repo"}, "revision", loreclient.Credential{},
		[]string{"never-matches"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || len(client.fileCalls) != int(codeSearchMaxTotalBytes/codeSearchMaxFileBytes) {
		t.Fatalf("truncation = %t, file calls = %d", result.Truncated, len(client.fileCalls))
	}
}

func TestCodeSearchRepositoryReaderPreservesDeniedAccess(t *testing.T) {
	api := &API{store: codeSearchStore{readErr: platform.ErrNotFound}}
	_, err := api.repositoryForCodeSearch(context.Background(), &platform.User{ID: "viewer"}, "acme", "private")
	if !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("repository lookup error = %v", err)
	}
}

func makeCodeSearchFiles(count int, size uint64) []loreclient.TreeEntry {
	entries := make([]loreclient.TreeEntry, count)
	for index := range entries {
		path := "file-" + string(rune('a'+index%26)) + "-" +
			strings.Repeat("0", index/26) + string(rune('a'+index%26)) + ".txt"
		entries[index] = loreclient.TreeEntry{Name: path, Path: path, Kind: "file", Size: size}
	}
	return entries
}

type codeSearchFakeFile struct {
	body   []byte
	binary bool
	size   uint64
}

type codeSearchFakeClient struct {
	trees     map[string]loreclient.Tree
	files     map[string]codeSearchFakeFile
	fileCalls []string
}

func (client *codeSearchFakeClient) Tree(
	_ context.Context,
	_ loreclient.RepositoryRef,
	_ string,
	path string,
	_ loreclient.Credential,
	_ int,
) (loreclient.Tree, error) {
	return client.trees[path], nil
}

func (client *codeSearchFakeClient) File(
	_ context.Context,
	_ loreclient.RepositoryRef,
	_ string,
	path string,
	_ loreclient.Credential,
	_ int64,
) (loreclient.File, []byte, error) {
	client.fileCalls = append(client.fileCalls, path)
	fixture := client.files[path]
	return loreclient.File{
		Path: path, Kind: "file", Size: fixture.size, Binary: fixture.binary, BinaryKnown: true,
	}, fixture.body, nil
}

func (client *codeSearchFakeClient) RevisionHistory(
	context.Context, loreclient.RepositoryRef, string, string, loreclient.Credential, int,
) ([]loreclient.RevisionHistoryEntry, error) {
	return nil, nil
}

func (client *codeSearchFakeClient) FileHistory(
	context.Context, loreclient.RepositoryRef, string, string, string, loreclient.Credential, int,
) ([]loreclient.FileHistoryEntry, error) {
	return nil, nil
}

func (client *codeSearchFakeClient) RevisionInfo(
	context.Context, loreclient.RepositoryRef, string, loreclient.Credential,
) (loreclient.Revision, error) {
	return loreclient.Revision{}, nil
}

func (client *codeSearchFakeClient) RevisionDiff(
	context.Context, loreclient.RepositoryRef, string, string, []string, loreclient.Credential, int, int,
) (loreclient.Diff, error) {
	return loreclient.Diff{}, nil
}

type codeSearchStore struct {
	fakeStore
	readErr    error
	repository platform.Repository
}

func (store codeSearchStore) RepositoryForRead(
	context.Context, *platform.User, string, string,
) (platform.Repository, error) {
	if store.readErr != nil {
		return platform.Repository{}, store.readErr
	}
	return store.repository, nil
}
