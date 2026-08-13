package httpapi

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
	"github.com/lorehub/lorehub/services/api/internal/runner"
)

type leaseCheckingRunnerControl struct {
	leaseholder string
}

func (*leaseCheckingRunnerControl) RunnerClaimJob(
	context.Context, []byte, string, time.Time, time.Duration,
) (*runner.Job, error) {
	return nil, nil
}

func (control *leaseCheckingRunnerControl) RunnerLeaseJob(
	_ context.Context, _ string, runnerID string,
) (runner.Job, error) {
	if runnerID != control.leaseholder {
		return runner.Job{}, runner.ErrRunnerLeaseNotHeld
	}
	return runner.Job{ID: "job-1", RunID: "run-1", Attempt: 1, RepositoryID: "repository-1"}, nil
}

func (control *leaseCheckingRunnerControl) RunnerHeartbeatJob(
	_ context.Context, _ string, runnerID string, _ time.Duration,
) error {
	if runnerID != control.leaseholder {
		return runner.ErrRunnerLeaseNotHeld
	}
	return nil
}

func (control *leaseCheckingRunnerControl) RunnerCancellationRequested(
	_ context.Context, _ string, runnerID string,
) (bool, error) {
	if runnerID != control.leaseholder {
		return false, runner.ErrRunnerLeaseNotHeld
	}
	return false, nil
}

func (control *leaseCheckingRunnerControl) AppendRunnerJobLog(
	context.Context, string, string, int64, []byte, int64,
) (int64, error) {
	return 0, nil
}

func (control *leaseCheckingRunnerControl) AppendRunnerArtifact(
	context.Context, string, string, string, int64, []byte, bool, int64, int64, int,
) (int64, error) {
	return 0, nil
}

func (control *leaseCheckingRunnerControl) CompleteJob(
	_ context.Context, _ runner.Job, runnerID string, _, _ string, _ []runner.Artifact,
) error {
	if runnerID != control.leaseholder {
		return runner.ErrRunnerLeaseNotHeld
	}
	return nil
}

func TestRunnerControlEndpointsRejectNonLeaseholder(t *testing.T) {
	codec, err := auth.NewSecretCodec("runner control endpoint test secret")
	if err != nil {
		t.Fatal(err)
	}
	credential, _, err := auth.NewRunnerCredential(codec)
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeRunnerStore{authenticated: platform.Runner{ID: "runner-presented"}}
	control := &leaseCheckingRunnerControl{leaseholder: "runner-other"}
	handler := New(
		fakeStore{}, fakeLore{}, &rejectingRunnerEndpointAuthenticator{}, healthy{}, "",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithRunners(store, codec, "runner-key-v1"),
		WithRunnerControlPlane(
			control,
			runner.NewFailClosedExecutionContextResolver(),
			runner.NewFailClosedJobTokenIssuer(),
			RunnerControlConfig{
				LeaseDuration: time.Minute, LogMaxBytes: 1024, ArtifactMaxCount: 1,
				ArtifactMaxFileBytes: 1024, ArtifactMaxTotalBytes: 1024,
				Principal: runner.CredentialPrincipal{Kind: "service", Subject: "runner"},
				RESTScope: runner.JobTokenRESTScope, GraphQLScope: runner.JobTokenGraphQLScope,
			},
		),
	)
	tests := []struct {
		method string
		path   string
		body   []byte
	}{
		{method: http.MethodPost, path: "/api/v1/actions/runner/jobs/job-1/heartbeat"},
		{method: http.MethodGet, path: "/api/v1/actions/runner/jobs/job-1/context"},
		{method: http.MethodPost, path: "/api/v1/actions/runner/jobs/job-1/token"},
		{
			method: http.MethodPost, path: "/api/v1/actions/runner/jobs/job-1/complete",
			body: []byte(`{"conclusion":"success"}`),
		},
	}
	for _, testCase := range tests {
		request := httptest.NewRequest(testCase.method, testCase.path, bytes.NewReader(testCase.body))
		request.Header.Set("Authorization", "Bearer "+credential)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusConflict {
			t.Fatalf("%s %s status=%d body=%s", testCase.method, testCase.path,
				response.Code, response.Body.String())
		}
	}
}
