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
	branches, err := client.Branches(ctx, RepositoryRef{CacheKey: "integration", URL: repositoryURL}, "")
	if err != nil {
		t.Fatalf("Branches returned an error: %v", err)
	}
	for _, branch := range branches {
		if branch.Name == repository.DefaultBranch && branch.LatestRevision != "" {
			return
		}
	}
	t.Fatalf("default branch %q was not returned: %#v", repository.DefaultBranch, branches)
}
