package loresagent

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ConfigFileName       = "config.json"
	CertificateFileName  = "hook-client.crt"
	PrivateKeyFileName   = "hook-client.key"
	RegistrationEndpoint = "/api/v1/lore-servers/register"
	HeartbeatEndpoint    = "/api/v1/lore-servers/heartbeat"
	CertificateEndpoint  = "/api/v1/lore-servers/certificate"
	RenewalWindow        = 7 * 24 * time.Hour
	maxResponseBytes     = 1 << 20
)

type Config struct {
	LoreHubURL string `json:"lorehubUrl"`
	Credential string `json:"credential"`
	ServerID   string `json:"serverId,omitempty"`
	Name       string `json:"name"`
}

type RegisterRequest struct {
	Name              string         `json:"name"`
	PublicURL         string         `json:"publicUrl"`
	LoreBuildVersion  string         `json:"loreBuildVersion"`
	HookModuleVersion string         `json:"hookModuleVersion"`
	HealthMetadata    map[string]any `json:"healthMetadata"`
}

type HeartbeatRequest struct {
	LoreBuildVersion  string         `json:"loreBuildVersion"`
	HookModuleVersion string         `json:"hookModuleVersion"`
	HealthMetadata    map[string]any `json:"healthMetadata"`
}

type Server struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	PublicURL        string         `json:"publicUrl"`
	Status           string         `json:"status"`
	LoreBuildVersion string         `json:"loreBuildVersion"`
	LastSeenAt       *time.Time     `json:"lastSeenAt"`
	HealthMetadata   map[string]any `json:"healthMetadata"`
}

type RegisterResponse struct {
	Server              Server    `json:"server"`
	Credential          string    `json:"credential"`
	CredentialExpiresAt time.Time `json:"credentialExpiresAt"`
}

type HeartbeatResponse struct {
	Server Server `json:"server"`
}

type CertificateResponse struct {
	CertificatePEM string    `json:"certificatePem"`
	PrivateKeyPEM  string    `json:"privateKeyPem"`
	Serial         string    `json:"serial"`
	IssuedAt       time.Time `json:"issuedAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
}

type APIError struct {
	StatusCode int
	Code       string
	Detail     string
}

func (err *APIError) Error() string {
	message := fmt.Sprintf("LoreHub API returned HTTP %d", err.StatusCode)
	if err.Code != "" {
		message += " (" + err.Code + ")"
	}
	if err.Detail != "" {
		message += ": " + err.Detail
	}
	return message
}

func IsAuthenticationError(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized
}

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("LoreHub URL must be an http or https URL without a query or fragment")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{BaseURL: baseURL, HTTPClient: httpClient}, nil
}

func (client *Client) Register(
	ctx context.Context,
	registrationToken string,
	input RegisterRequest,
) (RegisterResponse, error) {
	var response RegisterResponse
	if err := client.postJSON(ctx, RegistrationEndpoint, registrationToken, input, &response); err != nil {
		return RegisterResponse{}, err
	}
	return response, nil
}

func (client *Client) Heartbeat(
	ctx context.Context,
	credential string,
	input HeartbeatRequest,
) (HeartbeatResponse, error) {
	var response HeartbeatResponse
	if err := client.postJSON(ctx, HeartbeatEndpoint, credential, input, &response); err != nil {
		return HeartbeatResponse{}, err
	}
	return response, nil
}

func (client *Client) RenewCertificate(
	ctx context.Context,
	credential string,
) (CertificateResponse, error) {
	var response CertificateResponse
	if err := client.postJSON(ctx, CertificateEndpoint, credential, struct{}{}, &response); err != nil {
		return CertificateResponse{}, err
	}
	return response, nil
}

func (client *Client) postJSON(
	ctx context.Context,
	path string,
	credential string,
	input any,
	output any,
) error {
	body, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode LoreHub request: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, client.BaseURL+path, bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("create LoreHub request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(credential))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := client.HTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("call LoreHub API: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read LoreHub response: %w", err)
	}
	if len(responseBody) > maxResponseBytes {
		return errors.New("LoreHub response is too large")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return newAPIError(response.StatusCode, responseBody)
	}
	if output == nil || len(responseBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(responseBody, output); err != nil {
		return fmt.Errorf("decode LoreHub response: %w", err)
	}
	return nil
}

func newAPIError(statusCode int, body []byte) error {
	var response struct {
		Error struct {
			Code   string `json:"code"`
			Detail string `json:"detail"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return &APIError{StatusCode: statusCode}
	}
	return &APIError{
		StatusCode: statusCode,
		Code:       response.Error.Code,
		Detail:     response.Error.Detail,
	}
}

func ConfigPath(configDir string) string {
	return filepath.Join(configDir, ConfigFileName)
}

func CertificatePath(configDir string) string {
	return filepath.Join(configDir, CertificateFileName)
}

func PrivateKeyPath(configDir string) string {
	return filepath.Join(configDir, PrivateKeyFileName)
}

func SaveConfig(configDir string, config Config) error {
	if strings.TrimSpace(configDir) == "" {
		return errors.New("config directory is required")
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.Chmod(configDir, 0o700); err != nil {
		return fmt.Errorf("set config directory permissions: %w", err)
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	encoded = append(encoded, '\n')

	temporary, err := os.CreateTemp(configDir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set temporary config permissions: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	if err := os.Rename(temporaryPath, ConfigPath(configDir)); err != nil {
		return fmt.Errorf("install config: %w", err)
	}
	return nil
}

func SaveCertificate(configDir string, serverID string, response CertificateResponse) error {
	if strings.TrimSpace(configDir) == "" || strings.TrimSpace(serverID) == "" {
		return errors.New("config directory and Lore server ID are required")
	}
	certificatePEM := []byte(response.CertificatePEM)
	privateKeyPEM := []byte(response.PrivateKeyPEM)
	pair, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		return fmt.Errorf("validate Lore server hook certificate and key: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return errors.New("validate Lore server hook certificate: certificate is missing")
	}
	certificate, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return fmt.Errorf("parse Lore server hook certificate: %w", err)
	}
	if certificate.Subject.CommonName != "lore-server-"+serverID {
		return errors.New("Lore server hook certificate identity does not match the configured server")
	}
	if response.Serial == "" || certificate.SerialNumber.Text(16) != response.Serial ||
		response.IssuedAt.IsZero() || response.ExpiresAt.IsZero() ||
		!certificate.NotAfter.Equal(response.ExpiresAt) {
		return errors.New("Lore server hook certificate metadata is invalid")
	}
	clientAuth := false
	for _, usage := range certificate.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			clientAuth = true
			break
		}
	}
	if !clientAuth {
		return errors.New("Lore server hook certificate lacks client authentication usage")
	}
	if err := ensureConfigDirectory(configDir); err != nil {
		return err
	}
	keyTemporary, err := writePrivateTemporary(configDir, ".hook-client-key-*.tmp", privateKeyPEM)
	if err != nil {
		return err
	}
	defer os.Remove(keyTemporary)
	certificateTemporary, err := writePrivateTemporary(configDir, ".hook-client-cert-*.tmp", certificatePEM)
	if err != nil {
		return err
	}
	defer os.Remove(certificateTemporary)
	if err := os.Rename(keyTemporary, PrivateKeyPath(configDir)); err != nil {
		return fmt.Errorf("install Lore server hook private key: %w", err)
	}
	if err := os.Rename(certificateTemporary, CertificatePath(configDir)); err != nil {
		return fmt.Errorf("install Lore server hook certificate: %w", err)
	}
	return nil
}

func CertificateNeedsRenewal(configDir string, serverID string, now time.Time) (bool, error) {
	pair, err := tls.LoadX509KeyPair(CertificatePath(configDir), PrivateKeyPath(configDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, fmt.Errorf("load Lore server hook certificate: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return false, errors.New("load Lore server hook certificate: certificate is missing")
	}
	certificate, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return false, fmt.Errorf("parse Lore server hook certificate: %w", err)
	}
	if certificate.Subject.CommonName != "lore-server-"+serverID {
		return false, errors.New("Lore server hook certificate identity does not match the configured server")
	}
	return !certificate.NotAfter.After(now.UTC().Add(RenewalWindow)), nil
}

func ensureConfigDirectory(configDir string) error {
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.Chmod(configDir, 0o700); err != nil {
		return fmt.Errorf("set config directory permissions: %w", err)
	}
	return nil
}

func writePrivateTemporary(configDir string, pattern string, contents []byte) (string, error) {
	temporary, err := os.CreateTemp(configDir, pattern)
	if err != nil {
		return "", fmt.Errorf("create temporary certificate file: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("set temporary certificate file permissions: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("write temporary certificate file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("sync temporary certificate file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("close temporary certificate file: %w", err)
	}
	return temporaryPath, nil
}

func LoadConfig(configDir string) (Config, error) {
	file, err := os.Open(ConfigPath(configDir))
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer file.Close()
	var config Config
	decoder := json.NewDecoder(io.LimitReader(file, maxResponseBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("decode config: expected one JSON object")
	}
	if strings.TrimSpace(config.LoreHubURL) == "" || strings.TrimSpace(config.Credential) == "" {
		return Config{}, errors.New("config must contain lorehubUrl and credential")
	}
	return config, nil
}
