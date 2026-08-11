package lore

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestSDKClientAgainstLoreServer(t *testing.T) {
	// This fixture is unauthenticated component coverage, not production auth evidence.
	repositoryURL := os.Getenv("LOREHUB_TEST_LORE_URL")
	if repositoryURL == "" {
		t.Skip("LOREHUB_TEST_LORE_URL is not set")
	}
	client, err := NewDevelopmentSDKClient(loreTestTempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	bootstrapCredential := Credential{
		Partition: repositoryURLPartition(repositoryURL), Identity: "fixture", Scope: ScopeRead,
		Principal:           ServicePrincipal(ServicePurposeRepositoryRegistration, "fixture-registration"),
		InsecureDevelopment: true,
	}
	repository, err := client.RepositoryInfo(ctx, repositoryURL, bootstrapCredential)
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
	readCredential := developmentCredential(repository.ID, "fixture", ScopeRead)
	branches, err := client.Branches(ctx, ref, readCredential)
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
	tree, err := code.Tree(ctx, ref, latest, "", readCredential, 100)
	if err != nil {
		t.Fatalf("Tree returned an error: %v", err)
	}
	if tree.Revision != latest || !hasTreeEntry(tree, "README.md", "file") || !hasTreeEntry(tree, "src", "directory") {
		t.Fatalf("tree did not contain the expected exact-revision entries: %#v", tree)
	}
	file, body, err := code.File(ctx, ref, latest, "README.md", readCredential, 1<<20)
	if err != nil {
		t.Fatalf("File returned an error: %v", err)
	}
	if file.Revision != latest || file.Binary || string(body) == "" || file.Content == "" {
		t.Fatalf("file response was incomplete: file=%#v body=%q", file, body)
	}
	fileHistory, err := code.FileHistory(ctx, ref, latest, repository.DefaultBranch, "README.md", readCredential, 20)
	if err != nil {
		t.Fatalf("FileHistory returned an error: %v", err)
	}
	if len(fileHistory) == 0 || fileHistory[0].Path != "README.md" || fileHistory[0].Revision == "" {
		t.Fatalf("file history was incomplete: %#v", fileHistory)
	}
	history, err := code.RevisionHistory(ctx, ref, latest, repository.DefaultBranch, readCredential, 20)
	if err != nil {
		t.Fatalf("RevisionHistory returned an error: %v", err)
	}
	if len(history) < 2 || history[0].Revision != latest {
		t.Fatalf("revision history did not contain the fixture's two revisions: %#v", history)
	}
	detail, err := code.RevisionInfo(ctx, ref, latest, readCredential)
	if err != nil {
		t.Fatalf("RevisionInfo returned an error: %v", err)
	}
	if detail.Revision != latest {
		t.Fatalf("revision detail was incomplete: %#v", detail)
	}
	diff, err := code.RevisionDiff(ctx, ref, history[1].Revision, latest, nil, readCredential, 20, 1<<20)
	if err != nil {
		t.Fatalf("RevisionDiff returned an error: %v", err)
	}
	if diff.Source != history[1].Revision || diff.Target != latest || len(diff.Files) == 0 {
		t.Fatalf("revision diff did not contain a changed file: %#v", diff)
	}
	lockIdentity := "fixture-lock-user"
	writeCredential := Credential{
		Partition: repository.ID, Identity: lockIdentity, Scope: ScopeWrite,
		Principal: UserPrincipal(lockIdentity), InsecureDevelopment: true,
	}
	locked, err := client.AcquireFileLock(
		ctx, ref, repository.DefaultBranch, "README.md", writeCredential,
	)
	if err != nil {
		t.Fatalf("AcquireFileLock returned an error: %v", err)
	}
	defer func() {
		_, _ = client.ReleaseFileLock(
			context.Background(), ref, repository.DefaultBranch, "README.md", writeCredential, false,
		)
	}()
	if locked.Path != "README.md" || locked.OwnerID == "" || locked.BranchID == "" {
		t.Fatalf("acquired file lock is incomplete: %#v", locked)
	}
	locks, err := client.QueryFileLocks(ctx, ref, repository.DefaultBranch, "", "README.md", writeCredential)
	if err != nil || len(locks) != 1 || locks[0].Path != "README.md" {
		t.Fatalf("QueryFileLocks returned locks=%#v error=%v", locks, err)
	}
	if _, err := client.ReleaseFileLock(
		ctx, ref, repository.DefaultBranch, "README.md", writeCredential, false,
	); err != nil {
		t.Fatalf("ReleaseFileLock returned an error: %v", err)
	}
	locks, err = client.QueryFileLocks(ctx, ref, repository.DefaultBranch, "", "README.md", writeCredential)
	if err != nil || len(locks) != 0 {
		t.Fatalf("released file lock remained: locks=%#v error=%v", locks, err)
	}
}

func TestSDKClientLoreAuthBoundaryAgainstLoreServer(t *testing.T) {
	variables := []string{
		"LOREHUB_TEST_LORE_URL", "LOREHUB_TEST_LORE_OTHER_URL", "LOREHUB_TEST_LORE_AUTH_URL",
		"LOREHUB_TEST_LORE_IDENTITY", "LOREHUB_TEST_LORE_READ_TOKEN", "LOREHUB_TEST_LORE_OTHER_TOKEN",
		"LOREHUB_TEST_LORE_BASE_TOKEN", "LOREHUB_TEST_LORE_EXPIRED_TOKEN",
		"LOREHUB_TEST_LORE_WRONG_ISSUER_TOKEN", "LOREHUB_TEST_LORE_WRONG_AUDIENCE_TOKEN",
		"LOREHUB_TEST_LORE_WRONG_KID_TOKEN",
	}
	for _, variable := range variables {
		if os.Getenv(variable) == "" {
			t.Skipf("%s is not set", variable)
		}
	}
	authURL := os.Getenv("LOREHUB_TEST_LORE_AUTH_URL")
	parsedAuthURL, err := url.Parse(authURL)
	if err != nil || parsedAuthURL.Scheme != "ucs-auth" || parsedAuthURL.Host == "" {
		t.Fatalf("invalid test AuthURL: %q", authURL)
	}
	client, err := NewSDKClientWithAuthAuthority(loreTestTempDir(t), parsedAuthURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	identity := os.Getenv("LOREHUB_TEST_LORE_IDENTITY")
	firstURL := os.Getenv("LOREHUB_TEST_LORE_URL")
	secondURL := os.Getenv("LOREHUB_TEST_LORE_OTHER_URL")
	firstPartition := repositoryURLPartition(firstURL)
	secondPartition := repositoryURLPartition(secondURL)
	if firstPartition == "" || secondPartition == "" || firstPartition == secondPartition {
		t.Fatalf("test repositories must have two distinct partitions: %q, %q", firstURL, secondURL)
	}
	firstRef := RepositoryRef{CacheKey: "auth-boundary-first", URL: firstURL, LoreRepositoryID: firstPartition}
	secondRef := RepositoryRef{CacheKey: "auth-boundary-second", URL: secondURL, LoreRepositoryID: secondPartition}
	readCredential := boundaryCredential(os.Getenv("LOREHUB_TEST_LORE_READ_TOKEN"), authURL,
		os.Getenv("LOREHUB_TEST_LORE_BASE_TOKEN"), firstPartition, identity, ScopeRead)
	otherCredential := boundaryCredential(os.Getenv("LOREHUB_TEST_LORE_OTHER_TOKEN"), authURL,
		os.Getenv("LOREHUB_TEST_LORE_BASE_TOKEN"), secondPartition, identity, ScopeRead)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	repository, err := client.RepositoryInfo(ctx, firstURL, readCredential)
	if err != nil {
		t.Fatalf("valid gRPC repository read failed: %v", err)
	}
	if repository.ID != firstPartition {
		t.Fatalf("repository identity mismatch: got %q want %q", repository.ID, firstPartition)
	}
	branches, err := client.Branches(ctx, firstRef, readCredential)
	if err != nil || len(branches) == 0 {
		t.Fatalf("valid branch read failed: branches=%#v error=%v", branches, err)
	}
	latest := ""
	for _, branch := range branches {
		if branch.LatestRevision != "" {
			latest = branch.LatestRevision
			break
		}
	}
	if latest == "" {
		t.Fatal("the first repository must contain an observed revision for the QUIC read check")
	}
	tree, err := CodeClient(client).Tree(ctx, firstRef, latest, "", readCredential, 100)
	if err != nil || tree.Revision != latest {
		t.Fatalf("valid QUIC tree read failed: tree=%#v error=%v", tree, err)
	}
	if _, err := client.Branches(ctx, secondRef, otherCredential); err != nil {
		t.Fatalf("valid second-partition read failed: %v", err)
	}

	negativeTokens := []struct {
		name  string
		token string
	}{
		{name: "zero-resource-base", token: os.Getenv("LOREHUB_TEST_LORE_BASE_TOKEN")},
		{name: "expired", token: os.Getenv("LOREHUB_TEST_LORE_EXPIRED_TOKEN")},
		{name: "wrong-issuer", token: os.Getenv("LOREHUB_TEST_LORE_WRONG_ISSUER_TOKEN")},
		{name: "wrong-audience", token: os.Getenv("LOREHUB_TEST_LORE_WRONG_AUDIENCE_TOKEN")},
		{name: "wrong-kid", token: os.Getenv("LOREHUB_TEST_LORE_WRONG_KID_TOKEN")},
	}
	for _, testCase := range negativeTokens {
		t.Run(testCase.name, func(t *testing.T) {
			credential := boundaryCredential(os.Getenv("LOREHUB_TEST_LORE_READ_TOKEN"), authURL,
				testCase.token, firstPartition,
				identity+"-"+testCase.name, ScopeRead)
			if _, err := client.Branches(ctx, firstRef, credential); err == nil {
				t.Fatalf("%s token was accepted by the data plane", testCase.name)
			}
		})
	}
	for _, testCase := range []struct {
		name       string
		token      string
		partition  string
		repository RepositoryRef
	}{
		{name: "first-token-on-second", token: os.Getenv("LOREHUB_TEST_LORE_READ_TOKEN"),
			partition: secondPartition, repository: secondRef},
		{name: "second-token-on-first", token: os.Getenv("LOREHUB_TEST_LORE_OTHER_TOKEN"),
			partition: firstPartition, repository: firstRef},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			credential := boundaryCredential(testCase.token, authURL,
				os.Getenv("LOREHUB_TEST_LORE_BASE_TOKEN"), testCase.partition,
				identity+"-"+testCase.name, ScopeRead)
			if _, err := client.Branches(ctx, testCase.repository, credential); err == nil {
				t.Fatal("a token for another Lore partition was accepted")
			}
		})
	}
	readAsWrite := boundaryCredential(os.Getenv("LOREHUB_TEST_LORE_READ_TOKEN"), authURL,
		os.Getenv("LOREHUB_TEST_LORE_BASE_TOKEN"), firstPartition, identity+"-write", ScopeWrite)
	if err := client.CreateRepositoryWithCredential(ctx, firstURL, firstPartition, "blocked", "blocked",
		readAsWrite); err == nil {
		t.Fatal("a read-only token was accepted by the gRPC write path")
	}
}

func boundaryCredential(
	resourceToken string,
	authURL string,
	authenticationToken string,
	partition string,
	identity string,
	scope Scope,
) Credential {
	return Credential{
		Partition: partition, Scope: scope, ResourceID: "urc-" + partition, Subject: identity,
		RequestedScopes: []string{string(scope)}, GrantedScopes: []string{string(scope)}, Identity: identity,
		Token: resourceToken, AuthenticationToken: authenticationToken, AuthURL: authURL,
		ExpiresAt:               time.Now().UTC().Add(5 * time.Minute),
		AuthenticationExpiresAt: time.Now().UTC().Add(5 * time.Minute),
		Principal:               UserPrincipal(identity),
	}
}

func developmentCredential(partition, identity string, scope Scope) Credential {
	return Credential{
		Partition: partition, Identity: identity, Scope: scope,
		Principal: ServicePrincipal(ServicePurposePublicReader, "fixture-public-reader"), InsecureDevelopment: true,
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
