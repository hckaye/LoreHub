package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
	"github.com/lorehub/lorehub/services/api/internal/servercert"
)

type certificateStoreCapture struct {
	server   platform.LoreServer
	recorded servercert.Certificate
}

func (store *certificateStoreCapture) AuthenticateServer(
	context.Context, []byte, string, time.Time,
) (platform.LoreServer, error) {
	return store.server, nil
}

func (store *certificateStoreCapture) RecordServerCertificate(
	_ context.Context, serverID string, serial string, issuedAt time.Time, expiresAt time.Time,
) error {
	if serverID != store.server.ID {
		return platform.ErrInvalidInput
	}
	store.recorded = servercert.Certificate{Serial: serial, IssuedAt: issuedAt, ExpiresAt: expiresAt}
	return nil
}

type certificateIssuerStub struct {
	issued servercert.Certificate
}

func (issuer certificateIssuerStub) Issue(serverID string, _ time.Time) (servercert.Certificate, error) {
	return issuer.issued, nil
}

func TestIssueLoreServerCertificateAuthenticatesAndRecordsMetadata(t *testing.T) {
	secrets, err := auth.NewSecretCodec("Lore server certificate endpoint test secret")
	if err != nil {
		t.Fatal(err)
	}
	credential, _, err := auth.NewLoreServerCredential(secrets)
	if err != nil {
		t.Fatal(err)
	}
	issuedAt := time.Date(2026, time.August, 13, 4, 0, 0, 0, time.UTC)
	expiresAt := issuedAt.Add(30 * 24 * time.Hour)
	store := &certificateStoreCapture{server: platform.LoreServer{
		ID: "c727d690-34d4-4b44-bd13-a132f89c5919",
	}}
	api := &API{
		loreServerCertificates: store,
		loreServerCertIssuer: certificateIssuerStub{issued: servercert.Certificate{
			CertificatePEM: []byte("certificate PEM"), PrivateKeyPEM: []byte("private key PEM"),
			Serial: "abcdef1234", IssuedAt: issuedAt, ExpiresAt: expiresAt,
		}},
		loresSecrets: secrets, loresTokenKeyID: "test-lores-v1",
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/lore-servers/certificate", nil)
	request.Header.Set("Authorization", "Bearer "+credential)
	response := httptest.NewRecorder()

	api.issueLoreServerCertificate(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("certificate response status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
	var output struct {
		CertificatePEM string    `json:"certificatePem"`
		PrivateKeyPEM  string    `json:"privateKeyPem"`
		Serial         string    `json:"serial"`
		IssuedAt       time.Time `json:"issuedAt"`
		ExpiresAt      time.Time `json:"expiresAt"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.CertificatePEM != "certificate PEM" || output.PrivateKeyPEM != "private key PEM" ||
		output.Serial != "abcdef1234" || store.recorded.Serial != output.Serial ||
		!store.recorded.IssuedAt.Equal(issuedAt) || !store.recorded.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("certificate response = %+v, recorded = %+v", output, store.recorded)
	}
}
