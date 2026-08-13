package loresagent

import (
	"encoding/json"
	"io"
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
