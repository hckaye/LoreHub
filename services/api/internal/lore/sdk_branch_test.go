package lore

import "testing"

func TestValidBranchName(t *testing.T) {
	for _, name := range []string{"main", "feature/branch-management", "release-1.2", "機能/表示"} {
		if !ValidBranchName(name) {
			t.Errorf("valid branch name %q was rejected", name)
		}
	}
	for _, name := range []string{"", " feature", "feature ", "/main", "main/", "a//b", ".", "a/../b",
		"feature branch", "feature\\branch", "feature@branch"} {
		if ValidBranchName(name) {
			t.Errorf("invalid branch name %q was accepted", name)
		}
	}
}

func TestValidateBranchMutationRejectsInvalidBoundary(t *testing.T) {
	repository := RepositoryRef{
		CacheKey: "repository", URL: "lore://localhost/0123456789abcdef0123456789abcdef",
		LoreRepositoryID: "0123456789abcdef0123456789abcdef",
	}
	credential := Credential{
		Partition: repository.LoreRepositoryID, Scope: ScopeWrite, Identity: "user",
		Principal: UserPrincipal("user"), InsecureDevelopment: true,
	}
	validRevision := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := validateBranchMutation(repository, "feature/a", "feature", validRevision, credential); err != nil {
		t.Fatalf("valid branch mutation failed: %v", err)
	}
	for _, test := range []struct {
		name, category, revision string
	}{
		{name: "feature a", category: "feature", revision: validRevision},
		{name: "feature/a", category: "feature/category", revision: validRevision},
		{name: "feature/a", category: "feature", revision: "not-a-revision"},
	} {
		if err := validateBranchMutation(repository, test.name, test.category, test.revision, credential); err == nil {
			t.Fatalf("invalid branch mutation was accepted: %+v", test)
		}
	}
}

func TestValidateCreatedBranchState(t *testing.T) {
	validRevision := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := validateCreatedBranchState("branch-id", "feature/a", validRevision, "feature/a"); err != nil {
		t.Fatalf("valid created branch state failed: %v", err)
	}
	for _, test := range []struct {
		branchID, branchName, revision, expectedName string
	}{
		{branchName: "feature/a", revision: validRevision, expectedName: "feature/a"},
		{branchID: "branch-id", branchName: "other", revision: validRevision, expectedName: "feature/a"},
		{branchID: "branch-id", branchName: "feature/a", expectedName: "feature/a"},
	} {
		if err := validateCreatedBranchState(
			test.branchID, test.branchName, test.revision, test.expectedName,
		); err == nil {
			t.Fatalf("invalid created branch state was accepted: %+v", test)
		}
	}
}
