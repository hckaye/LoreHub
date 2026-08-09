package lore

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestCredentialProviderBindsPrincipalPartitionAndScope(t *testing.T) {
	provider, err := NewCredentialProvider("production", map[string]CredentialMaterial{
		"repo-1": {Identity: "svc-1", Token: "short-lived-token", AuthURL: "https://auth.example/login"},
	}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	ref := RepositoryRef{LoreRepositoryID: "repo-1"}
	readRequest := CredentialRequest{Principal: UserPrincipal("user-a"), Repository: ref, Scope: ScopeRead}
	read, err := provider.ForRepository(context.Background(), readRequest)
	if err != nil {
		t.Fatal(err)
	}
	if read.Partition != ref.LoreRepositoryID || read.Identity != "svc-1" || read.Token != "short-lived-token" ||
		read.AuthURL != "https://auth.example/login" || read.Scope != ScopeRead ||
		!read.Principal.equal(readRequest.Principal) || read.InsecureDevelopment {
		t.Fatal("production user credential did not preserve its contract")
	}
	publicRequest := CredentialRequest{
		Principal:  ServicePrincipal(ServicePurposePublicReader),
		Repository: ref,
		Scope:      ScopeRead,
	}
	public, err := provider.ForRepository(context.Background(), publicRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !public.Principal.equal(publicRequest.Principal) {
		t.Fatal("anonymous public read was not bound to the public-reader service purpose")
	}
	writeRequest := readRequest
	writeRequest.Scope = ScopeWrite
	write, err := provider.ForRepository(context.Background(), writeRequest)
	if err != nil || write.Scope != ScopeWrite || !write.Principal.equal(readRequest.Principal) {
		t.Fatal("write credential did not preserve the requested scope and principal")
	}
	unknown := readRequest
	unknown.Repository.LoreRepositoryID = "repo-2"
	if _, err := provider.ForRepository(context.Background(), unknown); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("partition mismatch error = %v, want ErrCredentialUnavailable", err)
	}
}

func TestValidateCredentialRejectsScopeWideningAndIdentityOnlyProduction(t *testing.T) {
	ref := RepositoryRef{LoreRepositoryID: "repo-1"}
	read := Credential{
		Partition: ref.LoreRepositoryID,
		Scope:     ScopeRead,
		Identity:  "identity",
		Token:     "token",
		AuthURL:   "https://auth.example/login",
		Principal: UserPrincipal("user-a"),
	}
	if err := ValidateCredential(ref, read, ScopeWrite); err == nil {
		t.Fatal("read credential was widened to write scope")
	}
	identityOnly := read
	identityOnly.Token = ""
	identityOnly.AuthURL = ""
	if err := ValidateCredential(ref, identityOnly, ScopeRead); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("production identity-only credential error = %v", err)
	}
	if _, err := NewCredentialProvider("production", map[string]CredentialMaterial{
		"repo-1": {Identity: "identity"},
	}, "", false); err == nil {
		t.Fatal("production accepted identity-only configured material")
	}
}

func TestDevelopmentCredentialIsExplicitlyInsecure(t *testing.T) {
	provider := NewDevelopmentCredentialProvider("fixture")
	request := CredentialRequest{
		Principal:  ServicePrincipal(ServicePurposePublicReader),
		Repository: RepositoryRef{LoreRepositoryID: "repo-1"},
		Scope:      ScopeWrite,
	}
	credential, err := provider.ForRepository(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !credential.InsecureDevelopment || credential.Identity != "fixture" || credential.Token != "" ||
		credential.AuthURL != "" || !credential.Principal.equal(request.Principal) {
		t.Fatal("development credential was not explicitly marked insecure")
	}
	if err := ValidateCredential(request.Repository, credential, request.Scope); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCredentialProvider("production", nil, "fixture", true); err == nil {
		t.Fatal("production accepted development fallback")
	}
	if _, err := NewCredentialProvider("production", nil, "fixture", false); err == nil {
		t.Fatal("production accepted identity-only fallback")
	}
}

func TestCredentialRequestRejectsAmbiguousPrincipal(t *testing.T) {
	provider, err := NewCredentialProvider("production", map[string]CredentialMaterial{
		"repo-1": {Identity: "id", Token: "token", AuthURL: "https://auth.example/login"},
	}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	request := CredentialRequest{
		Principal:  Principal{UserID: "user-a", ServicePurpose: ServicePurposePublicReader},
		Repository: RepositoryRef{LoreRepositoryID: "repo-1"},
		Scope:      ScopeRead,
	}
	if _, err := provider.ForRepository(context.Background(), request); !errors.Is(err, ErrInvalidPrincipal) {
		t.Fatalf("ambiguous principal error = %v, want ErrInvalidPrincipal", err)
	}
}

func TestCredentialProviderRejectsRepositoryPartitionMismatch(t *testing.T) {
	provider, err := NewCredentialProvider("production", map[string]CredentialMaterial{
		"repo-1": {Identity: "id", Token: "token", AuthURL: "https://auth.example/login"},
	}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	request := CredentialRequest{
		Principal:  UserPrincipal("user-a"),
		Repository: RepositoryRef{LoreRepositoryID: "repo-2"},
		Scope:      ScopeRead,
	}
	if _, err := provider.ForRepository(context.Background(), request); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("partition mismatch error = %v, want ErrCredentialUnavailable", err)
	}
}

func TestCredentialProviderIsSafeForConcurrentPrincipals(t *testing.T) {
	provider, err := NewCredentialProvider("production", map[string]CredentialMaterial{
		"repo-a": {Identity: "identity-a", Token: "token-a", AuthURL: "https://auth-a.example/login"},
		"repo-b": {Identity: "identity-b", Token: "token-b", AuthURL: "https://auth-b.example/login"},
	}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		principal Principal
		err       error
	}
	results := make(chan result, 2)
	requests := []struct {
		userID    string
		partition string
		token     string
		authURL   string
	}{
		{userID: "user-a", partition: "repo-a", token: "token-a", authURL: "https://auth-a.example/login"},
		{userID: "user-b", partition: "repo-b", token: "token-b", authURL: "https://auth-b.example/login"},
	}
	var group sync.WaitGroup
	for _, expected := range requests {
		expected := expected
		group.Add(1)
		go func() {
			defer group.Done()
			credential, credentialErr := provider.ForRepository(context.Background(), CredentialRequest{
				Principal:  UserPrincipal(expected.userID),
				Repository: RepositoryRef{LoreRepositoryID: expected.partition},
				Scope:      ScopeRead,
			})
			if credentialErr == nil && (credential.Partition != expected.partition || credential.Token != expected.token ||
				credential.AuthURL != expected.authURL) {
				credentialErr = errors.New("concurrent credential crossed repository partitions")
			}
			results <- result{principal: credential.Principal, err: credentialErr}
		}()
	}
	group.Wait()
	close(results)
	for item := range results {
		if item.err != nil {
			t.Fatal(item.err)
		}
		if item.principal.UserID != "user-a" && item.principal.UserID != "user-b" {
			t.Fatalf("credential principal crossed users: %+v", item.principal)
		}
	}
}
