package lore

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestSDKClientAgainstLoreServer(t *testing.T) {
	repositoryURL := os.Getenv("LOREHUB_TEST_LORE_URL")
	if repositoryURL == "" {
		t.Skip("LOREHUB_TEST_LORE_URL is not set")
	}
	client, err := NewSDKClient(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	repository, err := client.RepositoryInfo(ctx, repositoryURL, "")
	if err != nil {
		t.Fatalf("RepositoryInfo returned an error: %v", err)
	}
	if repository.ID == "" || repository.DefaultBranch == "" {
		t.Fatalf("repository data is incomplete: %#v", repository)
	}
	ref := RepositoryRef{
		CacheKey:         "integration",
		URL:              repositoryURL,
		LoreRepositoryID: repository.ID,
		DefaultBranch:    repository.DefaultBranch,
	}
	branches, err := client.Branches(ctx, ref, "")
	if err != nil {
		t.Fatalf("Branches returned an error: %v", err)
	}
	var latest string
	for _, branch := range branches {
		if branch.Name == repository.DefaultBranch && branch.LatestRevision != "" {
			latest = branch.LatestRevision
			break
		}
	}
	if latest == "" {
		t.Fatalf("default branch %q was not returned: %#v", repository.DefaultBranch, branches)
	}
	code := CodeClient(client)
	tree, err := code.Tree(ctx, ref, latest, "", "fixture", 100)
	if err != nil {
		t.Fatalf("Tree returned an error: %v", err)
	}
	if tree.Revision != latest || !hasTreeEntry(tree, "README.md", "file") || !hasTreeEntry(tree, "src", "directory") {
		t.Fatalf("tree did not contain the expected exact-revision entries: %#v", tree)
	}
	file, body, err := code.File(ctx, ref, latest, "README.md", "fixture", 1<<20)
	if err != nil {
		t.Fatalf("File returned an error: %v", err)
	}
	if file.Revision != latest || file.Binary || string(body) == "" || file.Content == "" {
		t.Fatalf("file response was incomplete: file=%#v body=%q", file, body)
	}
	fileHistory, err := code.FileHistory(ctx, ref, latest, repository.DefaultBranch, "README.md", "fixture", 20)
	if err != nil {
		t.Fatalf("FileHistory returned an error: %v", err)
	}
	if len(fileHistory) == 0 || fileHistory[0].Path != "README.md" || fileHistory[0].Revision == "" {
		t.Fatalf("file history was incomplete: %#v", fileHistory)
	}
	history, err := code.RevisionHistory(ctx, ref, latest, repository.DefaultBranch, "fixture", 20)
	if err != nil {
		t.Fatalf("RevisionHistory returned an error: %v", err)
	}
	if len(history) < 2 || history[0].Revision != latest {
		t.Fatalf("revision history did not contain the fixture's two revisions: %#v", history)
	}
	detail, err := code.RevisionInfo(ctx, ref, latest, "fixture")
	if err != nil {
		t.Fatalf("RevisionInfo returned an error: %v", err)
	}
	if detail.Revision != latest {
		t.Fatalf("revision detail was incomplete: %#v", detail)
	}
	diff, err := code.RevisionDiff(ctx, ref, history[1].Revision, latest, nil, "fixture", 20, 1<<20)
	if err != nil {
		t.Fatalf("RevisionDiff returned an error: %v", err)
	}
	if diff.Source != history[1].Revision || diff.Target != latest || len(diff.Files) == 0 {
		t.Fatalf("revision diff did not contain a changed file: %#v", diff)
	}
}

func hasTreeEntry(tree Tree, path string, kind string) bool {
	for _, entry := range tree.Entries {
		if entry.Path == path && entry.Kind == kind {
			return true
		}
	}
	return false
}
