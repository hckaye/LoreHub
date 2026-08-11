package lore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const maxCredentialLifetime = 15 * time.Minute

// AuthenticationTokenType selects LoreHub's signed base-token exchange in
// the UCS authentication API. Resource tokens are never sent through this
// external-token boundary.
const AuthenticationTokenType = "lorehub"

type Scope string

const (
	ScopeRead  Scope = "repository:read"
	ScopeWrite Scope = "repository:write"
)

const (
	ServicePurposePublicReader           = "public-reader"
	ServicePurposeActionsRunner          = "actions-runner"
	ServicePurposeObserver               = "observer"
	ServicePurposeRepositoryRegistration = "repository-registration"
)

// ServiceSubjects are configured JWT subjects for the service-purpose
// boundaries used by LoreHub. A purpose without its configured subject is not
// a valid production principal.
type ServiceSubjects struct {
	PublicReader           string
	ActionsRunner          string
	Observer               string
	RepositoryRegistration string
}

var (
	ErrCredentialUnavailable    = errors.New("Lore repository credential is unavailable")
	ErrCredentialIssuerRequired = errors.New("production Lore credential issuer is required")
	ErrInvalidPrincipal         = errors.New("Lore credential principal is invalid")
	ErrLoreAuthentication       = errors.New("Lore credential authentication failed")
	ErrCredentialContract       = errors.New("Lore credential contract is invalid")
)

// Principal identifies the caller that the control plane authorized. Users have
// only UserID; services have both a purpose and an immutable JWT subject.
type Principal struct {
	UserID         string
	ServicePurpose string
	Subject        string
}

func UserPrincipal(userID string) Principal {
	return Principal{UserID: strings.TrimSpace(userID)}
}

func ServicePrincipal(purpose string, subject string) Principal {
	return Principal{ServicePurpose: purpose, Subject: subject}
}

func (principal Principal) valid() bool {
	user := validPrincipalValue(principal.UserID) != ""
	service := validPrincipalValue(principal.ServicePurpose) != ""
	subject := validPrincipalValue(principal.Subject) != ""
	return (user && !service && !subject) || (!user && service && subject)
}

func (principal Principal) equal(other Principal) bool {
	return principal.UserID == other.UserID && principal.ServicePurpose == other.ServicePurpose &&
		principal.Subject == other.Subject
}

func (principal Principal) identity() string {
	if principal.UserID != "" {
		return principal.UserID
	}
	return principal.Subject
}

// CredentialMaterial is allowed only for explicit development and test
// providers. Production credentials are issued for each request instead.
type CredentialMaterial struct {
	Identity string `json:"identity"`
	Token    string `json:"token"`
	AuthURL  string `json:"authUrl"`
}

type CredentialRequest struct {
	Principal  Principal
	Repository RepositoryRef
	Partition  string
	Scope      Scope
}

type Credential struct {
	Partition       string   `json:"partition,omitempty"`
	Scope           Scope    `json:"scope,omitempty"`
	ResourceID      string   `json:"resourceId,omitempty"`
	Subject         string   `json:"-"`
	RequestedScopes []string `json:"requestedScopes,omitempty"`
	GrantedScopes   []string `json:"grantedScopes,omitempty"`
	Identity        string   `json:"-"`
	// Token is the exact resource-scoped Lore JWT used after exchange.
	Token string `json:"-"`
	// AuthenticationToken is a zero-resource JWT that the SDK exchanges through
	// the Lore UCS auth service before each repository operation.
	AuthenticationToken     string    `json:"-"`
	AuthURL                 string    `json:"-"`
	ExpiresAt               time.Time `json:"expiresAt,omitempty"`
	AuthenticationExpiresAt time.Time `json:"-"`
	Principal               Principal `json:"principal"`
	InsecureDevelopment     bool      `json:"insecureDevelopment,omitempty"`
}

// CredentialIssuer is the future control-plane boundary. Implementations must
// issue a credential for the complete request, not for a partition alone.
type CredentialIssuer interface {
	IssueCredential(context.Context, CredentialRequest) (Credential, error)
}

type CredentialIssuerFunc func(context.Context, CredentialRequest) (Credential, error)

func (issuer CredentialIssuerFunc) IssueCredential(
	ctx context.Context,
	request CredentialRequest,
) (Credential, error) {
	if issuer == nil {
		return Credential{}, ErrCredentialIssuerRequired
	}
	return issuer(ctx, request)
}

type CredentialProvider interface {
	ForRepository(context.Context, CredentialRequest) (Credential, error)
}

type configuredCredentialProvider struct {
	environment         string
	issuer              CredentialIssuer
	expectedAuthHost    string
	materials           map[string]CredentialMaterial
	developmentIdentity string
	allowDevelopment    bool
}

// NewProductionCredentialProvider creates the only provider permitted on the
// production path. The issuer is called on every request and is never cached.
func NewProductionCredentialProvider(
	issuer CredentialIssuer,
	expectedAuthHost string,
) (CredentialProvider, error) {
	return NewCredentialProviderWithIssuer("production", issuer, expectedAuthHost, nil, "", false)
}

// NewCredentialProviderWithIssuer keeps development configuration compatible
// while making production credentials injectable and request-scoped.
func NewCredentialProviderWithIssuer(
	environment string,
	issuer CredentialIssuer,
	expectedAuthHost string,
	materials map[string]CredentialMaterial,
	developmentIdentity string,
	allowDevelopmentFallback bool,
) (CredentialProvider, error) {
	if environment == "" {
		environment = "production"
	}
	if !isDevelopmentEnvironment(environment) {
		if issuer == nil {
			return nil, ErrCredentialIssuerRequired
		}
		if len(materials) != 0 {
			return nil, errors.New("static Lore credentials are only allowed in development or test")
		}
		if strings.TrimSpace(developmentIdentity) != "" || allowDevelopmentFallback {
			return nil, errors.New("development Lore credential fallback is not allowed in production")
		}
		if err := validateAuthAuthority(expectedAuthHost); err != nil {
			return nil, err
		}
		return configuredCredentialProvider{
			environment:      environment,
			issuer:           issuer,
			expectedAuthHost: expectedAuthHost,
		}, nil
	}

	clean := make(map[string]CredentialMaterial, len(materials))
	for partition, material := range materials {
		partition = strings.TrimSpace(partition)
		material.Identity = strings.TrimSpace(material.Identity)
		material.Token = strings.TrimSpace(material.Token)
		material.AuthURL = strings.TrimSpace(material.AuthURL)
		if partition == "" {
			return nil, errors.New("Lore credentials require non-empty partitions")
		}
		clean[partition] = material
	}
	developmentIdentity = strings.TrimSpace(developmentIdentity)
	if issuer != nil && len(clean) == 0 && !(allowDevelopmentFallback && developmentIdentity != "") {
		if err := validateAuthAuthority(expectedAuthHost); err != nil {
			return nil, err
		}
		return configuredCredentialProvider{
			environment: environment, issuer: issuer, expectedAuthHost: expectedAuthHost,
		}, nil
	}
	return configuredCredentialProvider{
		environment:         environment,
		materials:           clean,
		developmentIdentity: developmentIdentity,
		allowDevelopment:    allowDevelopmentFallback,
	}, nil
}

// NewCredentialProvider preserves the old constructor for development and
// test fixtures. Production callers must inject a CredentialIssuer explicitly.
func NewCredentialProvider(
	environment string,
	materials map[string]CredentialMaterial,
	developmentIdentity string,
	allowDevelopmentFallback bool,
) (CredentialProvider, error) {
	return NewCredentialProviderWithIssuer(environment, nil, "", materials, developmentIdentity,
		allowDevelopmentFallback)
}

func NewDevelopmentCredentialProvider(identity string) CredentialProvider {
	provider, _ := NewCredentialProvider("development", nil, identity, true)
	return provider
}

func (provider configuredCredentialProvider) ForRepository(
	ctx context.Context,
	request CredentialRequest,
) (Credential, error) {
	if err := ctx.Err(); err != nil {
		return Credential{}, err
	}
	partition, err := normalizeCredentialRequest(&request)
	if err != nil {
		return Credential{}, err
	}
	if provider.issuer != nil {
		issued, issueErr := provider.issuer.IssueCredential(ctx, request)
		if issueErr != nil {
			return Credential{}, fmt.Errorf("%w: issue request: %w", ErrCredentialUnavailable, issueErr)
		}
		if err := ctx.Err(); err != nil {
			return Credential{}, err
		}
		if err := validateIssuedCredential(request, issued, provider.expectedAuthHost); err != nil {
			return Credential{}, err
		}
		return issued, nil
	}
	if material, ok := provider.materials[partition]; ok {
		if material.Identity == "" {
			return Credential{}, ErrCredentialUnavailable
		}
		// Static material is deliberately not passed to the Lore auth store.
		// It is an explicit insecure fixture, even when extra JSON fields exist.
		return Credential{
			Partition:           partition,
			Scope:               request.Scope,
			Identity:            material.Identity,
			Principal:           request.Principal,
			InsecureDevelopment: true,
		}, nil
	}
	if isDevelopmentEnvironment(provider.environment) && provider.allowDevelopment &&
		provider.developmentIdentity != "" {
		return Credential{
			Partition:           partition,
			Scope:               request.Scope,
			Identity:            provider.developmentIdentity,
			Principal:           request.Principal,
			InsecureDevelopment: true,
		}, nil
	}
	return Credential{}, fmt.Errorf("%w for requested Lore partition and scope", ErrCredentialUnavailable)
}

func ParseCredentialMap(value string) (map[string]CredentialMaterial, error) {
	if strings.TrimSpace(value) == "" {
		return map[string]CredentialMaterial{}, nil
	}
	var result map[string]CredentialMaterial
	if err := json.Unmarshal([]byte(value), &result); err != nil || result == nil {
		return nil, errors.New("LOREHUB_LORE_CREDENTIALS must be a JSON object keyed by repository partition")
	}
	for partition, material := range result {
		if strings.TrimSpace(partition) == "" {
			return nil, errors.New("LOREHUB_LORE_CREDENTIALS contains an empty partition")
		}
		result[partition] = CredentialMaterial{
			Identity: strings.TrimSpace(material.Identity),
			Token:    strings.TrimSpace(material.Token),
			AuthURL:  strings.TrimSpace(material.AuthURL),
		}
	}
	return result, nil
}

func ValidateCredential(repository RepositoryRef, credential Credential, scope Scope) error {
	if scope != ScopeRead && scope != ScopeWrite {
		return errors.New("unsupported Lore credential scope")
	}
	if credential.Scope != scope && !(credential.Scope == ScopeWrite && scope == ScopeRead) {
		return fmt.Errorf("Lore credential scope %q does not permit %q", credential.Scope, scope)
	}
	if !credential.Principal.valid() {
		return ErrInvalidPrincipal
	}
	partition, err := repository.ValidatedPartition()
	if err != nil {
		return err
	}
	if partition != "" && credential.Partition != partition {
		return errors.New("Lore credential partition does not match repository")
	}
	if credential.Partition == "" && partition != "" {
		return errors.New("Lore credential partition is required")
	}
	if credential.InsecureDevelopment {
		if credential.Token != "" || credential.AuthURL != "" {
			return errors.New("insecure development credential cannot contain production auth material")
		}
		if credential.Identity == "" {
			return ErrCredentialUnavailable
		}
		return nil
	}
	return validateProductionCredential(credential, "")
}

func normalizeCredentialRequest(request *CredentialRequest) (string, error) {
	if !request.Principal.valid() {
		return "", ErrInvalidPrincipal
	}
	if request.Scope != ScopeRead && request.Scope != ScopeWrite {
		return "", errors.New("unsupported Lore credential scope")
	}
	partition, err := request.Repository.ValidatedPartition()
	if err != nil {
		return "", err
	}
	if request.Partition != "" && request.Partition != partition {
		return "", ErrCredentialContract
	}
	request.Partition = partition
	return partition, nil
}

func validPrincipalValue(value string) string {
	if value == "" || strings.TrimSpace(value) != value {
		return ""
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return ""
		}
	}
	return value
}

// ValidateServiceSubject validates the exact immutable subject configured for
// a service principal without exposing credential material or normalizing it.
func ValidateServiceSubject(subject string) error {
	if validPrincipalValue(subject) == "" {
		return ErrInvalidPrincipal
	}
	return nil
}

func validateIssuedCredential(
	request CredentialRequest,
	credential Credential,
	expectedAuthHost string,
) error {
	if credential.InsecureDevelopment || credential.Partition != request.Partition ||
		!credentialScopeNarrowed(request.Scope, credential.Scope) || !credential.Principal.equal(request.Principal) {
		return ErrCredentialContract
	}
	if credential.ResourceID != "urc-"+request.Partition || credential.Subject == "" ||
		credential.Identity == "" || credential.Token == "" || credential.AuthenticationToken == "" ||
		credential.AuthURL == "" {
		return ErrCredentialContract
	}
	if credential.Identity != credential.Subject || credential.Identity != request.Principal.identity() {
		return ErrCredentialContract
	}
	if !validScopeLists(request.Scope, credential.RequestedScopes, credential.GrantedScopes) {
		return ErrCredentialContract
	}
	now := time.Now().UTC()
	if !credential.ExpiresAt.After(now) || credential.ExpiresAt.After(now.Add(maxCredentialLifetime)) {
		return ErrCredentialContract
	}
	if !credential.AuthenticationExpiresAt.After(now) ||
		credential.AuthenticationExpiresAt.After(now.Add(maxCredentialLifetime)) {
		return ErrCredentialContract
	}
	if err := validateAuthURLAgainst(credential.AuthURL, expectedAuthHost); err != nil {
		return ErrCredentialContract
	}
	return nil
}

func validateProductionCredential(credential Credential, expectedAuthHost string) error {
	if !credential.Principal.valid() {
		return ErrInvalidPrincipal
	}
	if credential.Identity == "" || credential.Token == "" || credential.AuthenticationToken == "" ||
		credential.AuthURL == "" {
		return ErrCredentialUnavailable
	}
	if !credential.ExpiresAt.After(time.Now().UTC()) ||
		credential.ExpiresAt.After(time.Now().UTC().Add(maxCredentialLifetime)) {
		return ErrCredentialUnavailable
	}
	if !credential.AuthenticationExpiresAt.After(time.Now().UTC()) ||
		credential.AuthenticationExpiresAt.After(time.Now().UTC().Add(maxCredentialLifetime)) {
		return ErrCredentialUnavailable
	}
	if credential.Identity != credential.Principal.identity() {
		return ErrCredentialContract
	}
	if credential.Subject == "" || credential.Subject != credential.Identity ||
		!validPartitionSegment(credential.Partition) ||
		credential.ResourceID != "urc-"+credential.Partition ||
		!validCredentialScopeSubset(credential.RequestedScopes, credential.GrantedScopes) {
		return ErrCredentialContract
	}
	if err := validateAuthURLAgainst(credential.AuthURL, expectedAuthHost); err != nil {
		return err
	}
	return nil
}

func credentialScopeNarrowed(requested Scope, granted Scope) bool {
	return requested == granted
}

func validScopeLists(requested Scope, requestedScopes []string, grantedScopes []string) bool {
	if len(requestedScopes) == 0 || len(grantedScopes) == 0 {
		return false
	}
	requestedSet := make(map[string]bool, len(requestedScopes))
	for _, scope := range requestedScopes {
		if scope != string(ScopeRead) && scope != string(ScopeWrite) || requestedSet[scope] {
			return false
		}
		requestedSet[scope] = true
	}
	if !requestedSet[string(requested)] {
		return false
	}
	for _, scope := range grantedScopes {
		if (scope != string(ScopeRead) && scope != string(ScopeWrite)) || !requestedSet[scope] {
			return false
		}
	}
	return true
}

func validCredentialScopeSubset(requestedScopes []string, grantedScopes []string) bool {
	if len(requestedScopes) == 0 || len(grantedScopes) == 0 {
		return false
	}
	requestedSet := make(map[string]bool, len(requestedScopes))
	for _, scope := range requestedScopes {
		if scope != string(ScopeRead) && scope != string(ScopeWrite) || requestedSet[scope] {
			return false
		}
		requestedSet[scope] = true
	}
	for _, scope := range grantedScopes {
		if scope != string(ScopeRead) && scope != string(ScopeWrite) || !requestedSet[scope] {
			return false
		}
	}
	return true
}

func validateAuthAuthority(authority string) error {
	authority = strings.TrimSpace(authority)
	if authority == "" || strings.ContainsAny(authority, " /?#@\\\t\r\n") {
		return errors.New("production Lore auth authority is required")
	}
	parsed, err := url.Parse("ucs-auth://" + authority)
	if err != nil || parsed.Host != authority || parsed.Hostname() == "" || parsed.User != nil || parsed.Path != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return errors.New("production Lore auth authority is invalid")
	}
	if port := parsed.Port(); port != "" {
		portNumber, conversionErr := strconv.Atoi(port)
		if conversionErr != nil || portNumber < 1 || portNumber > 65535 {
			return errors.New("production Lore auth authority is invalid")
		}
	} else if strings.HasSuffix(authority, ":") {
		return errors.New("production Lore auth authority is invalid")
	}
	return nil
}

func ValidateAuthAuthority(authority string) error {
	return validateAuthAuthority(authority)
}

func ValidateAuthURL(value string) error {
	return validateAuthURL(value)
}

func validateAuthURL(value string) error {
	return validateAuthURLAgainst(value, "")
}

func validateAuthURLAgainst(value string, expectedAuthority string) error {
	if value == "" || strings.TrimSpace(value) != value ||
		strings.ContainsAny(value, "\x00\t\r\n ") {
		return errors.New("Lore AuthURL must use the ucs-auth scheme")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "ucs-auth" || parsed.Host == "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" || parsed.RawFragment != "" || parsed.Opaque != "" {
		return errors.New("Lore AuthURL must be a ucs-auth authority without credentials or path")
	}
	if err := validateAuthAuthority(parsed.Host); err != nil {
		return errors.New("Lore AuthURL authority is invalid")
	}
	if expectedAuthority == "" {
		return nil
	}
	if parsed.Host != expectedAuthority {
		return errors.New("Lore AuthURL authority does not match the configured Lore auth service")
	}
	return nil
}

func isDevelopmentEnvironment(environment string) bool {
	return environment == "development" || environment == "test"
}
