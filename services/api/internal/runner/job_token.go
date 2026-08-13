package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	JobTokenRESTScope    = "actions:job"
	JobTokenGraphQLScope = "actions:job"
)

type JobTokenRequest struct {
	JobID            string
	RunID            string
	Attempt          int
	RepositoryID     string
	ActorID          string
	ServicePrincipal CredentialPrincipal
	RESTScope        string
	GraphQLScope     string
	RequestedExpiry  time.Time
}

type JobToken struct {
	RepositoryID string    `json:"repositoryId"`
	Token        string    `json:"token"`
	Subject      string    `json:"subject"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type JobTokenIssuer interface {
	Issue(context.Context, JobTokenRequest) (JobToken, error)
}

type developmentJobTokenIssuer struct {
	token   string
	subject string
}

// NewDevelopmentJobTokenIssuer is an explicitly local/test-only token adapter.
func NewDevelopmentJobTokenIssuer(token string, subject string) JobTokenIssuer {
	return developmentJobTokenIssuer{token: token, subject: subject}
}

func (issuer developmentJobTokenIssuer) Issue(
	ctx context.Context,
	request JobTokenRequest,
) (JobToken, error) {
	if err := validateJobTokenRequest(ctx, request); err != nil {
		return JobToken{}, err
	}
	if strings.TrimSpace(issuer.token) == "" || strings.TrimSpace(issuer.subject) == "" {
		return JobToken{}, errors.New("development Actions job token is empty")
	}
	return JobToken{
		RepositoryID: request.RepositoryID,
		Token:        issuer.token,
		Subject:      issuer.subject,
		ExpiresAt:    time.Now().UTC().Add(5 * time.Minute),
	}, nil
}

type failClosedJobTokenIssuer struct{}

// NewFailClosedJobTokenIssuer is the safe placeholder until the control-plane issuer is injected.
func NewFailClosedJobTokenIssuer() JobTokenIssuer {
	return failClosedJobTokenIssuer{}
}

func (failClosedJobTokenIssuer) Issue(context.Context, JobTokenRequest) (JobToken, error) {
	return JobToken{}, errors.New("Actions job token issuer is not configured")
}

func issueJobToken(
	ctx context.Context,
	issuer JobTokenIssuer,
	job Job,
	principal CredentialPrincipal,
	restScope string,
	graphqlScope string,
) (JobToken, error) {
	if issuer == nil {
		return JobToken{}, errors.New("Actions job token issuer is not configured")
	}
	if restScope == "" {
		restScope = JobTokenRESTScope
	}
	if graphqlScope == "" {
		graphqlScope = JobTokenGraphQLScope
	}
	request := JobTokenRequest{
		JobID:            job.ID,
		RunID:            job.RunID,
		Attempt:          job.Attempt,
		RepositoryID:     job.RepositoryID,
		ActorID:          job.ActorID,
		ServicePrincipal: principal,
		RESTScope:        restScope,
		GraphQLScope:     graphqlScope,
		RequestedExpiry:  time.Now().UTC().Add(10 * time.Minute),
	}
	if err := validateJobTokenRequest(ctx, request); err != nil {
		return JobToken{}, err
	}
	token, err := issuer.Issue(ctx, request)
	if err != nil {
		return JobToken{}, fmt.Errorf("issue Actions job token: %w", err)
	}
	if err := validateIssuedJobToken(request, token); err != nil {
		return JobToken{}, err
	}
	return token, nil
}

func IssueJobToken(
	ctx context.Context,
	issuer JobTokenIssuer,
	job Job,
	principal CredentialPrincipal,
	restScope string,
	graphqlScope string,
) (JobToken, error) {
	return issueJobToken(ctx, issuer, job, principal, restScope, graphqlScope)
}

func validateJobTokenRequest(ctx context.Context, request JobTokenRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if request.JobID == "" || request.RunID == "" || request.RepositoryID == "" || request.Attempt <= 0 {
		return errors.New("exact Actions job, run, attempt, and repository are required")
	}
	if request.ServicePrincipal.Kind == "" || request.ServicePrincipal.Subject == "" {
		return errors.New("Actions service principal is required")
	}
	if request.RESTScope == "" || request.GraphQLScope == "" {
		return errors.New("Actions job token REST and GraphQL scopes are required")
	}
	now := time.Now().UTC()
	if request.RequestedExpiry.IsZero() || !request.RequestedExpiry.After(now) ||
		request.RequestedExpiry.After(now.Add(15*time.Minute)) {
		return errors.New("Actions job token expiry is outside its short-lived bound")
	}
	return nil
}

func validateIssuedJobToken(request JobTokenRequest, token JobToken) error {
	if strings.TrimSpace(token.Token) == "" || strings.TrimSpace(token.Subject) == "" {
		return errors.New("Actions job token issuer returned incomplete token material")
	}
	if strings.ContainsAny(token.Token, "\x00\r\n") || strings.ContainsAny(token.Subject, "\x00\r\n") {
		return errors.New("Actions job token contains unsafe characters")
	}
	if token.RepositoryID != request.RepositoryID {
		return errors.New("Actions job token does not match the requested repository")
	}
	if token.Subject != request.ServicePrincipal.Subject {
		return errors.New("Actions job token subject does not match the requested principal")
	}
	now := time.Now().UTC()
	if token.ExpiresAt.IsZero() || !token.ExpiresAt.After(now) || token.ExpiresAt.After(request.RequestedExpiry) {
		return errors.New("Actions job token is expired or exceeds the requested expiry")
	}
	return nil
}
