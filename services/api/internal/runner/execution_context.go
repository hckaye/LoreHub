package runner

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type GitHubContext struct {
	ServerURL  string
	APIURL     string
	GraphQLURL string
}

type ExecutionContextRequest struct {
	Principal      CredentialPrincipal
	RepositoryID   string
	OrganizationID string
	JobID          string
	Environment    string
	RequestedScope string
}

type ExecutionContext struct {
	OrganizationVariables map[string]string
	RepositoryVariables   map[string]string
	EnvironmentVariables  map[string]string
	OrganizationSecrets   map[string]string
	RepositorySecrets     map[string]string
	EnvironmentSecrets    map[string]string
}

type ResolvedExecutionContext struct {
	Variables map[string]string `json:"variables"`
	Secrets   map[string]string `json:"secrets"`
}

type ExecutionContextResolver interface {
	Resolve(context.Context, ExecutionContextRequest) (ExecutionContext, error)
}

type failClosedExecutionContextResolver struct{}

func NewFailClosedExecutionContextResolver() ExecutionContextResolver {
	return failClosedExecutionContextResolver{}
}

func (failClosedExecutionContextResolver) Resolve(context.Context, ExecutionContextRequest) (ExecutionContext, error) {
	return ExecutionContext{}, errors.New("Actions execution context resolver is not configured")
}

type developmentExecutionContextResolver struct {
	value ExecutionContext
}

// NewDevelopmentExecutionContextResolver is an explicit local/test adapter.
func NewDevelopmentExecutionContextResolver(value ExecutionContext) ExecutionContextResolver {
	return developmentExecutionContextResolver{value: value}
}

func (resolver developmentExecutionContextResolver) Resolve(
	ctx context.Context,
	request ExecutionContextRequest,
) (ExecutionContext, error) {
	if err := validateExecutionContextRequest(ctx, request); err != nil {
		return ExecutionContext{}, err
	}
	if err := validateExecutionContext(resolver.value); err != nil {
		return ExecutionContext{}, err
	}
	return resolver.value, nil
}

func resolveExecutionContext(
	ctx context.Context,
	resolver ExecutionContextResolver,
	request ExecutionContextRequest,
) (ResolvedExecutionContext, error) {
	if resolver == nil {
		return ResolvedExecutionContext{}, errors.New("Actions execution context resolver is not configured")
	}
	if err := validateExecutionContextRequest(ctx, request); err != nil {
		return ResolvedExecutionContext{}, err
	}
	value, err := resolver.Resolve(ctx, request)
	if err != nil {
		return ResolvedExecutionContext{}, fmt.Errorf("resolve Actions execution context: %w", err)
	}
	if err := validateExecutionContext(value); err != nil {
		return ResolvedExecutionContext{}, err
	}
	return ResolvedExecutionContext{
		Variables: mergeScopedValues(
			value.OrganizationVariables,
			value.RepositoryVariables,
			value.EnvironmentVariables,
		),
		Secrets: mergeScopedValues(
			value.OrganizationSecrets,
			value.RepositorySecrets,
			value.EnvironmentSecrets,
		),
	}, nil
}

func ResolveExecutionContext(
	ctx context.Context,
	resolver ExecutionContextResolver,
	request ExecutionContextRequest,
) (ResolvedExecutionContext, error) {
	return resolveExecutionContext(ctx, resolver, request)
}

func mergeScopedValues(scopes ...map[string]string) map[string]string {
	merged := make(map[string]string)
	for _, scope := range scopes {
		for name, value := range scope {
			merged[name] = value
		}
	}
	return merged
}

var executionNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,99}$`)

func validateExecutionContextRequest(ctx context.Context, request ExecutionContextRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if request.Principal.Kind == "" || request.Principal.Subject == "" {
		return errors.New("Actions execution principal is required")
	}
	if request.RepositoryID == "" || request.OrganizationID == "" {
		return errors.New("Actions execution repository and organization partitions are required")
	}
	if request.RequestedScope != "actions:execute" {
		return fmt.Errorf("Actions execution scope %q is not supported", request.RequestedScope)
	}
	if len(request.Environment) > 128 || strings.ContainsAny(request.Environment, "\x00\r\n") {
		return errors.New("Actions execution environment is invalid")
	}
	return nil
}

func validateExecutionContext(value ExecutionContext) error {
	for _, scope := range []map[string]string{
		value.OrganizationVariables, value.RepositoryVariables, value.EnvironmentVariables,
	} {
		if err := validateExecutionValues(scope, false); err != nil {
			return err
		}
	}
	for _, scope := range []map[string]string{
		value.OrganizationSecrets, value.RepositorySecrets, value.EnvironmentSecrets,
	} {
		if err := validateExecutionValues(scope, true); err != nil {
			return err
		}
	}
	return nil
}

func validateExecutionValues(scope map[string]string, secret bool) error {
	_ = secret
	for name, item := range scope {
		if !executionNamePattern.MatchString(name) || isReservedExecutionName(name) {
			return fmt.Errorf("Actions execution name %q is invalid or reserved", name)
		}
		if len(item) > 1<<20 || strings.ContainsRune(item, '\x00') {
			return fmt.Errorf("Actions execution value %q is invalid", name)
		}
	}
	return nil
}

func isReservedExecutionName(name string) bool {
	upper := strings.ToUpper(name)
	for _, prefix := range []string{"GITHUB_", "RUNNER_", "ACTIONS_", "DOCKER_"} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return upper == "CI" || upper == "PATH" || upper == "HOME" || upper == "HTTP_PROXY" ||
		upper == "HTTPS_PROXY" || upper == "NO_PROXY"
}

func validateGitHubContext(value GitHubContext, environment string) error {
	if strings.TrimSpace(value.ServerURL) == "" || strings.TrimSpace(value.APIURL) == "" ||
		strings.TrimSpace(value.GraphQLURL) == "" {
		return errors.New("GitHub-compatible public server, API, and GraphQL URLs are required")
	}
	server, err := validatePublicURL(value.ServerURL, environment, true)
	if err != nil {
		return err
	}
	for name, candidate := range map[string]string{
		"API": value.APIURL, "GraphQL": value.GraphQLURL,
	} {
		parsed, parseErr := validatePublicURL(candidate, environment, false)
		if parseErr != nil {
			return parseErr
		}
		if !strings.EqualFold(parsed.Scheme, server.Scheme) || !strings.EqualFold(parsed.Host, server.Host) {
			return fmt.Errorf("public %s URL must use the public server origin", name)
		}
	}
	return nil
}

func validatePublicURL(value string, environment string, originOnly bool) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimRight(value, "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" ||
		parsed.RawQuery != "" {
		return nil, errors.New("public Actions URLs must be absolute HTTP or HTTPS URLs")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("public Actions URLs must use HTTP or HTTPS")
	}
	if environment == "production" && parsed.Scheme != "https" {
		return nil, errors.New("public Actions URLs must use HTTPS in production")
	}
	if originOnly && parsed.Path != "" && parsed.Path != "/" {
		return nil, errors.New("public server URL must contain only an origin")
	}
	return parsed, nil
}

func writeSecretFile(directory string, secrets map[string]string) (string, error) {
	if len(secrets) == 0 {
		return "", nil
	}
	if directory == "" {
		return "", errors.New("secret file directory is required")
	}
	file, err := os.CreateTemp(directory, "actions-secrets-")
	if err != nil {
		return "", fmt.Errorf("create Actions secret file: %w", err)
	}
	path := file.Name()
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(path)
	}
	if err := file.Chmod(0o600); err != nil {
		cleanup()
		return "", fmt.Errorf("restrict Actions secret file: %w", err)
	}
	keys := make([]string, 0, len(secrets))
	for name := range secrets {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		if _, err := fmt.Fprintf(file, "%s=%s\n", name, encodeActSecretValue(secrets[name])); err != nil {
			cleanup()
			return "", fmt.Errorf("write Actions secret file: %w", err)
		}
	}
	if err := file.Sync(); err != nil {
		cleanup()
		return "", fmt.Errorf("sync Actions secret file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close Actions secret file: %w", err)
	}
	return filepath.Clean(path), nil
}

func encodeActSecretValue(value string) string {
	var builder strings.Builder
	builder.Grow(len(value) + 2)
	builder.WriteByte('"')
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '\\':
			builder.WriteString(`\\`)
		case '"':
			builder.WriteString(`\"`)
		case '$':
			builder.WriteString(`\$`)
		case '\r':
			builder.WriteString(`\r`)
		case '\n':
			builder.WriteString(`\n`)
		default:
			builder.WriteByte(value[index])
		}
	}
	builder.WriteByte('"')
	return builder.String()
}

func cleanupSecretFile(path string) {
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0o600)
	if err == nil {
		_ = file.Truncate(0)
		_ = file.Close()
	}
	_ = os.Remove(path)
}

func WriteActionSecretFile(directory string, secrets map[string]string) (string, error) {
	return writeSecretFile(directory, secrets)
}

func CleanupActionSecretFile(path string) {
	cleanupSecretFile(path)
}

func secretExpiry(now time.Time) time.Time {
	return now.Add(15 * time.Minute)
}
