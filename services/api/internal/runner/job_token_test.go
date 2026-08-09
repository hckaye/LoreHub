package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type recordingJobTokenIssuer struct {
	request JobTokenRequest
	token   JobToken
	err     error
}

func (issuer *recordingJobTokenIssuer) Issue(
	_ context.Context,
	request JobTokenRequest,
) (JobToken, error) {
	issuer.request = request
	if issuer.err != nil {
		return JobToken{}, issuer.err
	}
	return issuer.token, nil
}

func validJobTokenRequest() JobTokenRequest {
	return JobTokenRequest{
		JobID: "job", RunID: "run", Attempt: 2, RepositoryID: "repository",
		ActorID: "actor", ServicePrincipal: CredentialPrincipal{Kind: "service", Subject: "runner"},
		RESTScope: "actions:job", GraphQLScope: "actions:job",
		RequestedExpiry: time.Now().UTC().Add(10 * time.Minute),
	}
}

func TestIssueJobTokenCarriesExactExecutionContract(t *testing.T) {
	request := validJobTokenRequest()
	issuer := &recordingJobTokenIssuer{token: JobToken{
		RepositoryID: request.RepositoryID, Token: "opaque-token", Subject: "runner",
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	}}
	token, err := issueJobToken(
		context.Background(), issuer,
		Job{
			ID: request.JobID, RunID: request.RunID, Attempt: request.Attempt,
			RepositoryID: request.RepositoryID, ActorID: request.ActorID,
		},
		request.ServicePrincipal, request.RESTScope, request.GraphQLScope,
	)
	if err != nil || token.Token != "opaque-token" {
		t.Fatalf("unexpected job token: %#v, %v", token, err)
	}
	if issuer.request.JobID != request.JobID || issuer.request.RunID != request.RunID ||
		issuer.request.Attempt != request.Attempt || issuer.request.RepositoryID != request.RepositoryID ||
		issuer.request.ActorID != request.ActorID || issuer.request.ServicePrincipal != request.ServicePrincipal ||
		issuer.request.RESTScope != request.RESTScope || issuer.request.GraphQLScope != request.GraphQLScope {
		t.Fatalf("job token request lost exact execution context: %#v", issuer.request)
	}
}

func TestIssueJobTokenFailsClosedForMissingOrMismatchedMaterial(t *testing.T) {
	job := Job{ID: "job", RunID: "run", Attempt: 1, RepositoryID: "repository"}
	principal := CredentialPrincipal{Kind: "service", Subject: "runner"}
	for name, issuer := range map[string]JobTokenIssuer{
		"missing":     nil,
		"fail-closed": NewFailClosedJobTokenIssuer(),
		"error":       &recordingJobTokenIssuer{err: errors.New("issuer unavailable")},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := issueJobToken(context.Background(), issuer, job, principal, "rest", "graphql")
			if err == nil {
				t.Fatal("missing job token issuer was accepted")
			}
		})
	}
	bad := &recordingJobTokenIssuer{token: JobToken{
		RepositoryID: "other", Token: "token", Subject: "other",
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	}}
	_, err := issueJobToken(context.Background(), bad, job, principal, "rest", "graphql")
	if err == nil || !strings.Contains(err.Error(), "repository") {
		t.Fatalf("mismatched job token was accepted: %v", err)
	}
	partial := &recordingJobTokenIssuer{token: JobToken{RepositoryID: job.RepositoryID, Subject: "runner"}}
	_, err = issueJobToken(context.Background(), partial, job, principal, "rest", "graphql")
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("partial job token was accepted: %v", err)
	}
}

func TestDevelopmentJobTokenIssuerIsExplicitAndShortLived(t *testing.T) {
	request := validJobTokenRequest()
	issuer := NewDevelopmentJobTokenIssuer("development-token", "runner")
	token, err := issuer.Issue(context.Background(), request)
	if err != nil || token.Token != "development-token" || token.Subject != "runner" ||
		!token.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("unexpected development job token: %#v, %v", token, err)
	}
}
