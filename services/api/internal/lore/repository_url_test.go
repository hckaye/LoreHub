package lore

import (
	"testing"
	"time"
)

func TestProductionRepositoryURLRequiresTLSAndCanonicalPath(t *testing.T) {
	valid, err := parseRepositoryURL("lores://lore.example:41337/project", false)
	if err != nil || valid.Partition != "project" || valid.Authority != "lore.example:41337" {
		t.Fatalf("valid production URL = %+v, err=%v", valid, err)
	}
	invalid := []string{
		"lore://lore.example/project",
		"loresx://lore.example/project",
		"lores://user:password@lore.example/project",
		"lores://lore.example/project?token=secret",
		"lores://lore.example/project#fragment",
		"lores://lore.example/project/child",
		"lores://lore.example/project/..",
		"lores://lore.example/../project",
		"lores://lore.example/project/",
		"lores://lore.example/%70roject",
		"lores://lore.example:0/project",
		"lores://lore.example:abc/project",
	}
	for _, value := range invalid {
		if _, err := parseRepositoryURL(value, false); err == nil {
			t.Errorf("accepted invalid production URL %q", value)
		}
	}
}

func TestPlainRepositoryURLIsExplicitDevelopmentOnly(t *testing.T) {
	if _, err := parseRepositoryURL("lore://lore.example/project", false); err == nil {
		t.Fatal("production parser accepted plaintext Lore URL")
	}
	parsed, err := parseRepositoryURL("lore://lore.example/project", true)
	if err != nil || parsed.Partition != "project" {
		t.Fatalf("development parser rejected explicit plaintext URL: %+v, %v", parsed, err)
	}
}

func TestProductionSDKRejectsPlainRepositoryBeforeLoreCall(t *testing.T) {
	client, err := NewSDKClientWithAuthAuthority(t.TempDir(), "auth.example.com")
	if err != nil {
		t.Fatal(err)
	}
	credential := Credential{
		Partition: "project", Scope: ScopeRead, Identity: "user-a", Token: "token",
		AuthURL: "ucs-auth://auth.example.com", ExpiresAt: time.Now().UTC().Add(time.Minute),
		Principal: UserPrincipal("user-a"),
	}
	if _, err := client.RepositoryInfo(t.Context(), "lore://lore.example/project", credential); err == nil {
		t.Fatal("production SDK accepted a plaintext Lore repository URL")
	}
}

func TestAuthURLRequiresStockLoreFormatAndAuthority(t *testing.T) {
	if err := validateAuthURLAgainst("ucs-auth://auth.example.com", "auth.example.com"); err != nil {
		t.Fatal(err)
	}
	invalid := []string{
		"https://auth.example.com/login",
		"ucs-auth://:443",
		"ucs-auth://user:password@auth.example.com",
		"ucs-auth://auth.example.com/login",
		"ucs-auth://auth.example.com?tenant=one",
		"ucs-auth://auth.example.com#fragment",
	}
	for _, value := range invalid {
		if err := validateAuthURLAgainst(value, "auth.example.com"); err == nil {
			t.Errorf("accepted invalid production AuthURL %q", value)
		}
	}
	if err := validateAuthURLAgainst("ucs-auth://other.example.com", "auth.example.com"); err == nil {
		t.Fatal("accepted AuthURL with the wrong authority")
	}
}
