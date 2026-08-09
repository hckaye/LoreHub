package loreauth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/lorehub/lorehub/services/api/internal/authz"
)

type SigningKeyProvider interface {
	Current() jose.JSONWebKey
	PublicKeys() []jose.JSONWebKey
}

type RSAKeyProvider struct {
	current  jose.JSONWebKey
	previous []jose.JSONWebKey
}

func NewRSAKeyProvider(
	path string,
	encodedKey string,
	kid string,
	previous string,
	allowGenerate bool,
) (*RSAKeyProvider, error) {
	if strings.TrimSpace(kid) == "" {
		return nil, errors.New("LOREHUB_AUTH_SIGNING_KEY_KID is required")
	}
	key, err := loadPrivateKey(path, encodedKey, allowGenerate)
	if err != nil {
		return nil, err
	}
	provider := &RSAKeyProvider{current: jose.JSONWebKey{
		Key:       key,
		KeyID:     kid,
		Use:       "sig",
		Algorithm: string(jose.RS256),
	}}
	for _, entry := range strings.Split(previous, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, errors.New("LOREHUB_AUTH_PREVIOUS_KEYS must use kid=path entries")
		}
		previousKey, err := loadPublicKey(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("load previous signing key: %w", err)
		}
		provider.previous = append(provider.previous, jose.JSONWebKey{
			Key:       previousKey,
			KeyID:     strings.TrimSpace(parts[0]),
			Use:       "sig",
			Algorithm: string(jose.RS256),
		})
	}
	return provider, nil
}

func (provider *RSAKeyProvider) Current() jose.JSONWebKey {
	return provider.current
}

func (provider *RSAKeyProvider) PublicKeys() []jose.JSONWebKey {
	keys := make([]jose.JSONWebKey, 0, len(provider.previous)+1)
	keys = append(keys, provider.current.Public())
	keys = append(keys, provider.previous...)
	return keys
}

func loadPrivateKey(path string, encodedKey string, allowGenerate bool) (*rsa.PrivateKey, error) {
	var data []byte
	if strings.TrimSpace(encodedKey) != "" {
		data = []byte(encodedKey)
		if decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedKey)); err == nil {
			data = decoded
		}
	} else if strings.TrimSpace(path) != "" {
		cleanPath := filepath.Clean(path)
		read, err := os.ReadFile(cleanPath)
		if err != nil {
			if !allowGenerate || !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("read signing key: %w", err)
			}
			key, generateErr := rsa.GenerateKey(rand.Reader, 3072)
			if generateErr != nil {
				return nil, fmt.Errorf("generate local signing key: %w", generateErr)
			}
			if err := os.MkdirAll(filepath.Dir(cleanPath), 0o700); err != nil {
				return nil, fmt.Errorf("create signing key directory: %w", err)
			}
			data = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
			if err := os.WriteFile(cleanPath, data, 0o600); err != nil {
				return nil, fmt.Errorf("write local signing key: %w", err)
			}
			return key, nil
		}
		if info, statErr := os.Stat(cleanPath); statErr != nil {
			return nil, fmt.Errorf("stat signing key: %w", statErr)
		} else if info.Mode().Perm()&0o077 != 0 {
			return nil, errors.New("signing key file must not be readable by group or other users")
		}
		data = read
	} else {
		return nil, errors.New("an asymmetric Lore signing key path or secret is required")
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("signing key is not PEM encoded")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse signing key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("signing key must be RSA")
	}
	return key, nil
}

func loadPublicKey(path string) (any, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read previous public key: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("previous public key is not PEM encoded")
	}
	if publicKey, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if _, ok := publicKey.(*rsa.PublicKey); !ok {
			return nil, errors.New("previous public key must be RSA")
		}
		return publicKey, nil
	}
	if publicKey, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return publicKey, nil
	}
	if privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return privateKey.Public(), nil
	}
	return nil, errors.New("parse previous public key")
}

type LoreResourcePermission struct {
	ResourceID string   `json:"resource_id"`
	Permission []string `json:"permission"`
}

type LoreClaims struct {
	jwt.Claims
	Environment       string                   `json:"env"`
	Name              string                   `json:"name"`
	PreferredUsername string                   `json:"preferred_username"`
	Resources         []LoreResourcePermission `json:"resources,omitempty"`
	Groups            []string                 `json:"groups,omitempty"`
	IsServiceAccount  bool                     `json:"is_service_account"`
	IDP               string                   `json:"idp"`
}

type VerifiedToken struct {
	Claims LoreClaims
}

type TokenService struct {
	keys        SigningKeyProvider
	issuer      string
	audience    string
	environment string
	idp         string
	lifetime    time.Duration
}

func NewTokenService(
	keys SigningKeyProvider,
	issuer string,
	audience string,
	environment string,
	idp string,
	lifetime time.Duration,
) (*TokenService, error) {
	if keys == nil {
		return nil, errors.New("Lore signing key provider is required")
	}
	if issuer == "" || audience == "" {
		return nil, errors.New("Lore JWT issuer and audience are required")
	}
	if lifetime < 5*time.Minute || lifetime > 10*time.Minute {
		return nil, errors.New("Lore JWT lifetime must be between five and ten minutes")
	}
	if idp == "" {
		idp = "keycloak"
	}
	return &TokenService{keys: keys, issuer: issuer, audience: audience,
		environment: environment, idp: idp, lifetime: lifetime}, nil
}

func (service *TokenService) MintResourceToken(
	user authz.UserInfo,
	resources []LoreResourcePermission,
) (string, time.Time, error) {
	if len(resources) == 0 {
		return "", time.Time{}, errors.New("a Lore token must contain an exact resource scope")
	}
	if err := validateResources(resources); err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(service.lifetime)
	claims := LoreClaims{
		Claims: jwt.Claims{
			Subject:  user.ID,
			Issuer:   service.issuer,
			IssuedAt: jwt.NewNumericDate(now),
			Expiry:   jwt.NewNumericDate(expiresAt),
			Audience: jwt.Audience{service.audience},
		},
		Environment:       service.environment,
		Name:              user.DisplayName,
		PreferredUsername: user.Username,
		Resources:         resources,
		IsServiceAccount:  false,
		IDP:               service.idp,
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: service.keys.Current().Key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", service.keys.Current().KeyID))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create Lore JWT signer: %w", err)
	}
	token, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign Lore JWT: %w", err)
	}
	return token, expiresAt, nil
}

func (service *TokenService) Verify(raw string) (VerifiedToken, error) {
	if strings.TrimSpace(raw) == "" {
		return VerifiedToken{}, errors.New("empty Lore token")
	}
	parsed, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return VerifiedToken{}, errors.New("invalid Lore token")
	}
	if len(parsed.Headers) != 1 || parsed.Headers[0].Algorithm != string(jose.RS256) ||
		parsed.Headers[0].KeyID == "" {
		return VerifiedToken{}, errors.New("invalid Lore token header")
	}
	var claims LoreClaims
	verified := false
	for _, key := range service.keys.PublicKeys() {
		if key.KeyID != parsed.Headers[0].KeyID {
			continue
		}
		if err := parsed.Claims(key.Key, &claims); err == nil {
			verified = true
			break
		}
	}
	if !verified {
		return VerifiedToken{}, errors.New("invalid Lore token signature")
	}
	if err := claims.Validate(jwt.Expected{
		Issuer:      service.issuer,
		AnyAudience: jwt.Audience{service.audience},
		Time:        time.Now().UTC(),
	}); err != nil || claims.Expiry == nil || !claims.Expiry.Time().After(time.Now().UTC()) {
		return VerifiedToken{}, errors.New("invalid Lore token claims")
	}
	if claims.Issuer != service.issuer || len(claims.Audience) != 1 ||
		claims.Audience[0] != service.audience {
		return VerifiedToken{}, errors.New("invalid Lore token issuer or audience")
	}
	if claims.Subject == "" || claims.IsServiceAccount || claims.IDP == "" {
		return VerifiedToken{}, errors.New("invalid Lore token identity")
	}
	if len(claims.Resources) == 0 {
		return VerifiedToken{}, errors.New("Lore token has no resource scope")
	}
	if err := validateResources(claims.Resources); err != nil {
		return VerifiedToken{}, err
	}
	return VerifiedToken{Claims: claims}, nil
}

func validateResources(resources []LoreResourcePermission) error {
	seen := make(map[string]bool, len(resources))
	for _, resource := range resources {
		if !authz.ValidResourceID(resource.ResourceID) || seen[resource.ResourceID] || len(resource.Permission) == 0 {
			return errors.New("invalid Lore token resource")
		}
		seen[resource.ResourceID] = true
		for _, permission := range resource.Permission {
			if permission != authz.PermissionRead && permission != authz.PermissionWrite &&
				permission != authz.PermissionAdmin && permission != authz.PermissionObliterate {
				return errors.New("invalid Lore token permission")
			}
		}
	}
	return nil
}

func (service *TokenService) JWKS() map[string]any {
	keys := make([]jose.JSONWebKey, 0, len(service.keys.PublicKeys()))
	for _, key := range service.keys.PublicKeys() {
		keys = append(keys, key)
	}
	return map[string]any{"keys": keys}
}
