package lore

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingCredentialIssuer struct {
	mu       sync.Mutex
	requests []CredentialRequest
	issue    func(CredentialRequest) Credential
	err      error
}

func (issuer *recordingCredentialIssuer) IssueCredential(
	_ context.Context,
	request CredentialRequest,
) (Credential, error) {
	issuer.mu.Lock()
	issuer.requests = append(issuer.requests, request)
	issuer.mu.Unlock()
	if issuer.err != nil {
		return Credential{}, issuer.err
	}
	return issuer.issue(request), nil
}

func productionCredential(request CredentialRequest, token string) Credential {
	identity := request.Principal.identity()
	return Credential{
		Partition: request.Partition,
		Scope:     request.Scope,
		Identity:  identity,
		Token:     token,
		AuthURL:   "ucs-auth://auth.example.com",
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
		Principal: request.Principal,
	}
}

func productionProvider(t *testing.T, issuer CredentialIssuer) CredentialProvider {
	t.Helper()
	provider, err := NewProductionCredentialProvider(issuer, "auth.example.com")
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestProductionCredentialIssuerBindsEveryRequest(t *testing.T) {
	issuer := &recordingCredentialIssuer{}
	var sequence atomic.Int32
	issuer.issue = func(request CredentialRequest) Credential {
		return productionCredential(request, "token-"+request.Principal.identity()+"-"+
			string(request.Scope)+"-"+strconv.FormatInt(int64(sequence.Add(1)), 10))
	}
	provider := productionProvider(t, issuer)
	ref := RepositoryRef{LoreRepositoryID: "repo-a"}
	requestA := CredentialRequest{Principal: UserPrincipal("user-a"), Repository: ref, Scope: ScopeRead}
	first, err := provider.ForRepository(context.Background(), requestA)
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.ForRepository(context.Background(), requestA)
	if err != nil {
		t.Fatal(err)
	}
	if first.Token == second.Token || first.ExpiresAt.IsZero() || first.InsecureDevelopment {
		t.Fatalf("credentials were cached or insecure: first=%+v second=%+v", first, second)
	}
	requestB := CredentialRequest{
		Principal:  UserPrincipal("user-b"),
		Repository: RepositoryRef{LoreRepositoryID: "repo-b"},
		Scope:      ScopeWrite,
	}
	userB, err := provider.ForRepository(context.Background(), requestB)
	if err != nil {
		t.Fatal(err)
	}
	serviceRequest := CredentialRequest{
		Principal:  ServicePrincipal(ServicePurposePublicReader, "public-reader-subject"),
		Repository: ref,
		Scope:      ScopeRead,
	}
	service, err := provider.ForRepository(context.Background(), serviceRequest)
	if err != nil {
		t.Fatal(err)
	}
	if userB.Principal != requestB.Principal || userB.Partition != "repo-b" || userB.Scope != ScopeWrite {
		t.Fatalf("user B credential crossed request boundary: %+v", userB)
	}
	if service.Principal != serviceRequest.Principal || service.Identity != serviceRequest.Principal.Subject ||
		service.Partition != "repo-a" {
		t.Fatalf("service credential crossed request boundary: %+v", service)
	}
	issuer.mu.Lock()
	defer issuer.mu.Unlock()
	if len(issuer.requests) != 4 || issuer.requests[0].Partition != "repo-a" ||
		issuer.requests[2].Partition != "repo-b" || issuer.requests[3].Principal != serviceRequest.Principal ||
		issuer.requests[3].Principal.Subject != "public-reader-subject" {
		t.Fatalf("issuer did not receive canonical exact requests: %+v", issuer.requests)
	}
}

func TestCredentialRequestRejectsNonCanonicalPartitionAndPrincipal(t *testing.T) {
	issuer := &recordingCredentialIssuer{issue: func(request CredentialRequest) Credential {
		return productionCredential(request, "token")
	}}
	provider := productionProvider(t, issuer)
	base := CredentialRequest{
		Principal: UserPrincipal("user-a"), Repository: RepositoryRef{LoreRepositoryID: "repo-a"},
		Scope: ScopeRead,
	}
	partitionMismatch := base
	partitionMismatch.Partition = "repo-b"
	if _, err := provider.ForRepository(context.Background(), partitionMismatch); !errors.Is(err, ErrCredentialContract) {
		t.Fatalf("partition mismatch error = %v, want ErrCredentialContract", err)
	}
	for name, request := range map[string]CredentialRequest{
		"ambiguous user service": {Principal: Principal{UserID: "user-a", ServicePurpose: "service",
			Subject: "service-subject"},
			Repository: base.Repository, Scope: ScopeRead},
		"service without subject": {Principal: Principal{ServicePurpose: "service"},
			Repository: base.Repository, Scope: ScopeRead},
		"service empty subject": {Principal: ServicePrincipal("service", ""),
			Repository: base.Repository, Scope: ScopeRead},
		"service whitespace subject": {Principal: ServicePrincipal("service", " subject "),
			Repository: base.Repository, Scope: ScopeRead},
		"service control subject": {Principal: ServicePrincipal("service", "subject\x00value"),
			Repository: base.Repository, Scope: ScopeRead},
		"empty partition": {Principal: UserPrincipal("user-a"), Repository: RepositoryRef{}, Scope: ScopeRead},
		"unsafe partition": {Principal: UserPrincipal("user-a"),
			Repository: RepositoryRef{LoreRepositoryID: "../repo"}, Scope: ScopeRead},
	} {
		name, request := name, request
		t.Run(name, func(t *testing.T) {
			_, err := provider.ForRepository(context.Background(), request)
			if err == nil || (!errors.Is(err, ErrInvalidPrincipal) && !errors.Is(err, ErrCredentialContract) &&
				!strings.Contains(err.Error(), "partition")) {
				t.Fatalf("request error = %v, want a contract rejection", err)
			}
		})
	}
}

func TestServiceSubjectValidationRejectsInvalidValues(t *testing.T) {
	for name, subject := range map[string]string{
		"empty":          "",
		"space":          " ",
		"newline":        "subject\nvalue",
		"nul":            "subject\x00value",
		"vertical tab":   "subject\vvalue",
		"internal space": "subject value",
		"trailing":       "subject ",
	} {
		name, subject := name, subject
		t.Run(name, func(t *testing.T) {
			if ValidateServiceSubject(subject) == nil {
				t.Fatalf("accepted invalid service subject %q", subject)
			}
			if ServicePrincipal(ServicePurposePublicReader, subject).valid() {
				t.Fatalf("accepted invalid service principal subject %q", subject)
			}
		})
	}
	if !ServicePrincipal(ServicePurposePublicReader, "public-reader-jwt-subject").valid() {
		t.Fatal("rejected exact service subject")
	}
}

func TestProductionCredentialIssuerRejectsMismatchAndExpiry(t *testing.T) {
	request := CredentialRequest{Principal: UserPrincipal("user-a"), Repository: RepositoryRef{
		LoreRepositoryID: "repo-a",
	}, Scope: ScopeRead}
	cases := map[string]func(Credential) Credential{
		"partition": func(value Credential) Credential { value.Partition = "repo-other"; return value },
		"principal": func(value Credential) Credential { value.Principal = UserPrincipal("user-other"); return value },
		"scope":     func(value Credential) Credential { value.Scope = ScopeWrite; return value },
		"identity":  func(value Credential) Credential { value.Identity = "other-user"; return value },
		"expired": func(value Credential) Credential {
			value.ExpiresAt = time.Now().UTC().Add(-time.Second)
			return value
		},
		"too-long": func(value Credential) Credential {
			value.ExpiresAt = time.Now().UTC().Add(maxCredentialLifetime + time.Second)
			return value
		},
		"authority": func(value Credential) Credential {
			value.AuthURL = "ucs-auth://other.example.com"
			return value
		},
	}
	for name, mutate := range cases {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			issuer := &recordingCredentialIssuer{}
			issuer.issue = func(request CredentialRequest) Credential {
				return mutate(productionCredential(request, "never-log-this-token"))
			}
			_, err := productionProvider(t, issuer).ForRepository(context.Background(), request)
			if !errors.Is(err, ErrCredentialContract) {
				t.Fatalf("error = %v, want ErrCredentialContract", err)
			}
			if strings.Contains(err.Error(), "never-log-this-token") || strings.Contains(err.Error(), "other.example") {
				t.Fatalf("credential material leaked in error: %v", err)
			}
		})
	}
}

func TestProductionCredentialIssuerRequiresExactServiceSubject(t *testing.T) {
	request := CredentialRequest{
		Principal:  ServicePrincipal(ServicePurposePublicReader, "public-reader-jwt-subject"),
		Repository: RepositoryRef{LoreRepositoryID: "repo-a"},
		Scope:      ScopeRead,
	}
	cases := map[string]func(Credential) Credential{
		"wrong subject in principal": func(value Credential) Credential {
			value.Principal.Subject = "other-jwt-subject"
			return value
		},
		"wrong service identity": func(value Credential) Credential {
			value.Identity = "public-reader"
			return value
		},
		"wrong purpose and subject": func(value Credential) Credential {
			value.Principal = ServicePrincipal(ServicePurposeActionsRunner, "actions-jwt-subject")
			value.Identity = value.Principal.Subject
			return value
		},
	}
	for name, mutate := range cases {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			issuer := &recordingCredentialIssuer{issue: func(value CredentialRequest) Credential {
				return mutate(productionCredential(value, "service-token"))
			}}
			_, err := productionProvider(t, issuer).ForRepository(context.Background(), request)
			if !errors.Is(err, ErrCredentialContract) {
				t.Fatalf("error = %v, want ErrCredentialContract", err)
			}
			if strings.Contains(err.Error(), "service-token") {
				t.Fatalf("service token leaked in error: %v", err)
			}
		})
	}
}

func TestProductionCredentialIssuerIsRequiredAndStaticMapIsRejected(t *testing.T) {
	if _, err := NewProductionCredentialProvider(nil, "auth.example.com"); !errors.Is(err, ErrCredentialIssuerRequired) {
		t.Fatalf("missing issuer error = %v, want ErrCredentialIssuerRequired", err)
	}
	if _, err := NewCredentialProvider("production", map[string]CredentialMaterial{
		"repo-a": {Identity: "shared", Token: "token", AuthURL: "ucs-auth://auth.example.com"},
	}, "", false); !errors.Is(err, ErrCredentialIssuerRequired) {
		t.Fatalf("static production error = %v, want ErrCredentialIssuerRequired", err)
	}
}

func TestProductionServiceSubjectsRemainDistinct(t *testing.T) {
	issuer := &recordingCredentialIssuer{issue: func(request CredentialRequest) Credential {
		return productionCredential(request, "token-"+request.Principal.Subject)
	}}
	provider := productionProvider(t, issuer)
	base := RepositoryRef{LoreRepositoryID: "repo-a"}
	first, err := provider.ForRepository(context.Background(), CredentialRequest{
		Principal:  ServicePrincipal(ServicePurposePublicReader, "public-reader-subject-a"),
		Repository: base, Scope: ScopeRead,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.ForRepository(context.Background(), CredentialRequest{
		Principal:  ServicePrincipal(ServicePurposePublicReader, "public-reader-subject-b"),
		Repository: base, Scope: ScopeRead,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Principal == second.Principal || first.Identity == second.Identity || first.Token == second.Token {
		t.Fatalf("service credentials were reused across exact subjects: first=%+v second=%+v", first, second)
	}
}

func TestValidateCredentialRejectsScopeWideningAndIdentityOnly(t *testing.T) {
	ref := RepositoryRef{LoreRepositoryID: "repo-a"}
	read := productionCredential(CredentialRequest{
		Principal:  UserPrincipal("user-a"),
		Repository: ref,
		Partition:  "repo-a",
		Scope:      ScopeRead,
	}, "token")
	if err := ValidateCredential(ref, read, ScopeWrite); err == nil {
		t.Fatal("read credential was widened to write scope")
	}
	identityOnly := read
	identityOnly.Token = ""
	identityOnly.AuthURL = ""
	if err := ValidateCredential(ref, identityOnly, ScopeRead); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("production identity-only error = %v", err)
	}
}

func TestDevelopmentCredentialIsExplicitlyInsecure(t *testing.T) {
	provider, err := NewCredentialProvider("development", map[string]CredentialMaterial{
		"repo-a": {Identity: "fixture", Token: "ignored", AuthURL: "https://dev.invalid"},
	}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := provider.ForRepository(context.Background(), CredentialRequest{
		Principal:  ServicePrincipal(ServicePurposePublicReader, "public-reader-dev-subject"),
		Repository: RepositoryRef{LoreRepositoryID: "repo-a"},
		Scope:      ScopeWrite,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !credential.InsecureDevelopment || credential.Token != "" || credential.AuthURL != "" {
		t.Fatalf("development credential was not isolated: %+v", credential)
	}
	if err := ValidateCredential(RepositoryRef{LoreRepositoryID: "repo-a"}, credential, ScopeWrite); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialProviderIsSafeForConcurrentPrincipals(t *testing.T) {
	issuer := &recordingCredentialIssuer{}
	issuer.issue = func(request CredentialRequest) Credential {
		return productionCredential(request, "token-"+request.Principal.identity())
	}
	provider := productionProvider(t, issuer)
	requests := []CredentialRequest{
		{Principal: UserPrincipal("user-a"), Repository: RepositoryRef{LoreRepositoryID: "repo-a"}, Scope: ScopeRead},
		{Principal: UserPrincipal("user-b"), Repository: RepositoryRef{LoreRepositoryID: "repo-b"}, Scope: ScopeWrite},
		{Principal: ServicePrincipal(ServicePurposePublicReader, "public-reader-subject"),
			Repository: RepositoryRef{LoreRepositoryID: "repo-c"}, Scope: ScopeRead},
		{Principal: ServicePrincipal(ServicePurposeActionsRunner, "actions-runner-subject"),
			Repository: RepositoryRef{LoreRepositoryID: "repo-d"}, Scope: ScopeRead},
	}
	var group sync.WaitGroup
	results := make(chan Credential, len(requests))
	for _, request := range requests {
		request := request
		group.Add(1)
		go func() {
			defer group.Done()
			credential, err := provider.ForRepository(context.Background(), request)
			if err != nil {
				t.Errorf("issue credential: %v", err)
				return
			}
			results <- credential
		}()
	}
	group.Wait()
	close(results)
	for credential := range results {
		if !strings.Contains(credential.Token, credential.Principal.identity()) || credential.Partition == "" ||
			credential.Identity != credential.Principal.identity() {
			t.Fatalf("concurrent credential crossed users or partitions: %+v", credential)
		}
	}
}
