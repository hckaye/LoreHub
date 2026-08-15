package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type fakeStore struct {
	repositories []platform.Repository
	user         platform.User
}

func (store fakeStore) EnsureUser(context.Context, auth.Principal) (platform.User, error) {
	return store.user, nil
}

func (store fakeStore) ActiveUser(context.Context, string) (platform.User, error) {
	return store.user, nil
}

func (store fakeStore) CreateOrganization(
	context.Context,
	platform.User,
	platform.CreateOrganizationInput,
) (platform.Organization, error) {
	return platform.Organization{}, nil
}

func (store fakeStore) RegisterRepository(
	context.Context,
	platform.User,
	string,
	platform.RegisterRepositoryInput,
) (platform.Repository, error) {
	return platform.Repository{}, nil
}

func (store fakeStore) ExploreRepositories(context.Context, int) ([]platform.Repository, error) {
	return store.repositories, nil
}

func (store fakeStore) PublicRepository(context.Context, string, string) (platform.Repository, error) {
	return platform.Repository{}, platform.ErrNotFound
}

func (store fakeStore) RepositoryForWrite(
	context.Context,
	platform.User,
	string,
	string,
) (platform.Repository, error) {
	return platform.Repository{}, platform.ErrForbidden
}

func (store fakeStore) ListPublicIssues(context.Context, string, string, string) ([]platform.Issue, error) {
	return []platform.Issue{}, nil
}

func (store fakeStore) CreateIssue(
	context.Context,
	platform.User,
	string,
	string,
	platform.CreateIssueInput,
) (platform.Issue, error) {
	return platform.Issue{}, nil
}

func (store fakeStore) ListPublicMergeRequests(
	context.Context,
	string,
	string,
	string,
) ([]platform.MergeRequest, error) {
	return []platform.MergeRequest{}, nil
}

func (store fakeStore) CreateMergeRequest(
	context.Context,
	platform.User,
	string,
	string,
	platform.CreateMergeRequestInput,
) (platform.MergeRequest, error) {
	return platform.MergeRequest{}, nil
}

func (store fakeStore) ListPublicCIRuns(context.Context, string, string) ([]platform.CIRun, error) {
	return []platform.CIRun{}, nil
}

type fakeLore struct{}

func (fakeLore) RepositoryInfo(context.Context, string, loreclient.Credential) (loreclient.Repository, error) {
	return loreclient.Repository{}, nil
}

func (fakeLore) Branches(
	context.Context, loreclient.RepositoryRef, loreclient.Credential,
) ([]loreclient.Branch, error) {
	return []loreclient.Branch{}, nil
}

type healthy struct{}

func (healthy) Ping(context.Context) error { return nil }

type unhealthy struct{}

func (unhealthy) Ping(context.Context) error { return errors.New("dependency unavailable") }

func TestPlatformErrorMapsResourceLimits(t *testing.T) {
	api := &API{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, testCase := range []struct {
		err  error
		code string
	}{
		{platform.ErrOrganizationLimit, "organization_limit"},
		{platform.ErrRepositoryLimit, "repository_limit"},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/organizations", nil)
		api.platformError(response, request, "test", testCase.err)
		if response.Code != http.StatusConflict {
			t.Fatalf("%s status = %d, want 409, body = %s", testCase.code, response.Code, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), testCase.code) {
			t.Fatalf("%s body = %s", testCase.code, response.Body.String())
		}
	}
}

func TestReadyReflectsServiceDependencies(t *testing.T) {
	for name, testCase := range map[string]struct {
		health HealthChecker
		status int
	}{
		"ready":     {health: healthy{}, status: http.StatusOK},
		"not-ready": {health: unhealthy{}, status: http.StatusServiceUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			api := &API{health: testCase.health}
			response := httptest.NewRecorder()
			api.ready(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
			if response.Code != testCase.status {
				t.Fatalf("readiness status = %d, want %d", response.Code, testCase.status)
			}
		})
	}
}

func TestExploreRepositories(t *testing.T) {
	t.Parallel()
	handler := New(
		fakeStore{repositories: []platform.Repository{{ID: "repository-1", Slug: "lore"}}},
		fakeLore{},
		auth.DisabledAuthenticator{},
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/explore/repositories", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers were not applied")
	}
}

func TestOperationalMetricsRequireTokenAndUseRoutePatterns(t *testing.T) {
	t.Parallel()
	handler := New(
		fakeStore{repositories: []platform.Repository{}},
		fakeLore{},
		auth.DisabledAuthenticator{},
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithOperationalEndpoints(OperationalOptions{
			MetricsToken:      strings.Repeat("m", 32),
			RateLimitRequests: 10,
			RateLimitWindow:   time.Minute,
		}),
	)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/explore/repositories", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("explore status = %d", response.Code)
	}

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized metrics status = %d", unauthorized.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("m", 32))
	metrics := httptest.NewRecorder()
	handler.ServeHTTP(metrics, request)
	if metrics.Code != http.StatusOK {
		t.Fatalf("metrics status = %d", metrics.Code)
	}
	body := metrics.Body.String()
	if !strings.Contains(body,
		`lorehub_http_requests_total{method="GET",route="/api/v1/explore/repositories",status="200"} 1`) {
		t.Fatalf("metrics did not contain the normalized route:\n%s", body)
	}
}

func TestRateLimitRejectsExcessRequests(t *testing.T) {
	t.Parallel()
	handler := New(
		fakeStore{repositories: []platform.Repository{}},
		fakeLore{},
		auth.DisabledAuthenticator{},
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithOperationalEndpoints(OperationalOptions{
			RateLimitRequests: 2,
			RateLimitWindow:   time.Minute,
		}),
	)

	for attempt := 1; attempt <= 3; attempt++ {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/explore/repositories", nil)
		request.RemoteAddr = "192.0.2.10:1234"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if attempt < 3 && response.Code != http.StatusOK {
			t.Fatalf("attempt %d status = %d", attempt, response.Code)
		}
		if attempt == 3 && (response.Code != http.StatusTooManyRequests ||
			response.Header().Get("Retry-After") == "") {
			t.Fatalf("rate-limited response = %d headers=%v", response.Code, response.Header())
		}
	}
}

func TestRateLimitTrustsForwardedAddressOnlyFromConfiguredProxy(t *testing.T) {
	prefix := netip.MustParsePrefix("10.0.0.0/8")
	limiter := &fixedWindowLimiter{trustedProxies: []netip.Prefix{prefix}}

	trusted := httptest.NewRequest(http.MethodGet, "/api/v1/account", nil)
	trusted.RemoteAddr = "10.0.0.2:443"
	trusted.Header.Set("X-Forwarded-For", "198.51.100.9, 10.0.0.3")
	if got := limiter.clientAddress(trusted); got != netip.MustParseAddr("198.51.100.9") {
		t.Fatalf("trusted proxy client address = %s", got)
	}

	untrusted := httptest.NewRequest(http.MethodGet, "/api/v1/account", nil)
	untrusted.RemoteAddr = "192.0.2.5:443"
	untrusted.Header.Set("X-Forwarded-For", "198.51.100.9")
	if got := limiter.clientAddress(untrusted); got != netip.MustParseAddr("192.0.2.5") {
		t.Fatalf("untrusted proxy client address = %s", got)
	}
}
