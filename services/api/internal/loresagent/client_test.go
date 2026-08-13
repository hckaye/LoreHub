package loresagent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRegisterExchangesRegistrationToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != RegistrationEndpoint {
			t.Fatalf("unexpected register request: %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer lhsr_registration" {
			t.Fatalf("authorization = %q", got)
		}
		var input RegisterRequest
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Fatalf("decode register request: %v", err)
		}
		if input.Name != "storage-1" || input.PublicURL != "lores://storage.example:41337" ||
			input.LoreBuildVersion != "0.8.6" || input.HookModuleVersion != "1.0.0" {
			t.Fatalf("unexpected register request: %+v", input)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, `{"server":{"id":"server-1","name":"storage-1"},"credential":"lhss_credential","credentialExpiresAt":"2027-08-13T00:00:00Z"}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Register(t.Context(), "lhsr_registration", RegisterRequest{
		Name:              "storage-1",
		PublicURL:         "lores://storage.example:41337",
		LoreBuildVersion:  "0.8.6",
		HookModuleVersion: "1.0.0",
		HealthMetadata:    map[string]any{"state": "configured"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Server.ID != "server-1" || response.Credential != "lhss_credential" {
		t.Fatalf("unexpected register response: %+v", response)
	}
}

func TestHeartbeatReportsAuthenticationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != HeartbeatEndpoint {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer lhss_invalid" {
			t.Fatalf("authorization = %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(writer, `{"error":{"code":"invalid_credential","detail":"The Lore server credential is invalid"}}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Heartbeat(t.Context(), "lhss_invalid", HeartbeatRequest{
		LoreBuildVersion:  "0.8.6",
		HookModuleVersion: "1.0.0",
		HealthMetadata:    map[string]any{"uptimeSeconds": 4},
	})
	if err == nil || !IsAuthenticationError(err) {
		t.Fatalf("heartbeat error = %v, want authentication error", err)
	}
	if got := err.Error(); got != "LoreHub API returned HTTP 401 (invalid_credential): The Lore server credential is invalid" {
		t.Fatalf("heartbeat error = %q", got)
	}
}

func TestRenewCertificateStoresPrivateCertificatePair(t *testing.T) {
	serverID := "c727d690-34d4-4b44-bd13-a132f89c5919"
	issuedAt := time.Date(2026, time.August, 13, 4, 0, 0, 0, time.UTC)
	issued := testCertificateResponse(t, serverID, issuedAt, issuedAt.Add(30*24*time.Hour))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != CertificateEndpoint {
			t.Fatalf("unexpected certificate request: %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer lhss_credential" {
			t.Fatalf("authorization = %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(issued); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.RenewCertificate(t.Context(), "lhss_credential")
	if err != nil {
		t.Fatal(err)
	}
	configDir := t.TempDir()
	if err := SaveCertificate(configDir, serverID, response); err != nil {
		t.Fatal(err)
	}
	if mode := fileMode(t, CertificatePath(configDir)); mode != 0o600 {
		t.Fatalf("certificate mode = %04o, want 0600", mode)
	}
	if mode := fileMode(t, PrivateKeyPath(configDir)); mode != 0o600 {
		t.Fatalf("private key mode = %04o, want 0600", mode)
	}
	needsRenewal, err := CertificateNeedsRenewal(configDir, serverID, issuedAt)
	if err != nil {
		t.Fatal(err)
	}
	if needsRenewal {
		t.Fatal("new certificate unexpectedly needs renewal")
	}
	needsRenewal, err = CertificateNeedsRenewal(configDir, serverID, issued.ExpiresAt.Add(-RenewalWindow))
	if err != nil {
		t.Fatal(err)
	}
	if !needsRenewal {
		t.Fatal("certificate inside the renewal window was not selected for renewal")
	}
}

func TestCertificateNeedsRenewalWhenCertificateIsMissing(t *testing.T) {
	needsRenewal, err := CertificateNeedsRenewal(
		t.TempDir(), "c727d690-34d4-4b44-bd13-a132f89c5919", time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !needsRenewal {
		t.Fatal("missing certificate was not selected for renewal")
	}
}

func TestSaveConfigUsesPrivatePermissions(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "agent")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := Config{
		LoreHubURL: "https://lorehub.example",
		Credential: "lhss_secret",
		ServerID:   "server-1",
		Name:       "storage-1",
	}
	if err := SaveConfig(configDir, config); err != nil {
		t.Fatal(err)
	}
	if mode := fileMode(t, configDir); mode != 0o700 {
		t.Fatalf("config directory mode = %04o, want 0700", mode)
	}
	if mode := fileMode(t, ConfigPath(configDir)); mode != 0o600 {
		t.Fatalf("config file mode = %04o, want 0600", mode)
	}
	loaded, err := LoadConfig(configDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != config {
		t.Fatalf("loaded config = %+v, want %+v", loaded, config)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestLoadConfigRejectsUnknownFields(t *testing.T) {
	configDir := t.TempDir()
	if err := os.WriteFile(ConfigPath(configDir), []byte(`{"lorehubUrl":"https://example","credential":"lhss_x","name":"x","extra":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(configDir); err == nil {
		t.Fatal("LoadConfig accepted an unknown field")
	}
}

func TestRegisterResponseAcceptsCredentialExpiry(t *testing.T) {
	response := RegisterResponse{CredentialExpiresAt: time.Date(2027, time.August, 13, 0, 0, 0, 0, time.UTC)}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var decoded RegisterResponse
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.CredentialExpiresAt.Equal(response.CredentialExpiresAt) {
		t.Fatalf("credential expiry = %v, want %v", decoded.CredentialExpiresAt, response.CredentialExpiresAt)
	}
}

func testCertificateResponse(
	t *testing.T,
	serverID string,
	issuedAt time.Time,
	expiresAt time.Time,
) CertificateResponse {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(0x1234),
		Subject:      pkix.Name{CommonName: "lore-server-" + serverID},
		NotBefore:    issuedAt.Add(-time.Minute),
		NotAfter:     expiresAt,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	certificateDER, err := x509.CreateCertificate(
		rand.Reader, template, template, &privateKey.PublicKey, privateKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return CertificateResponse{
		CertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})),
		PrivateKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})),
		Serial:         template.SerialNumber.Text(16),
		IssuedAt:       issuedAt,
		ExpiresAt:      expiresAt,
	}
}
