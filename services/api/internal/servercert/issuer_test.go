package servercert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func TestIssueEncodesLoreServerID(t *testing.T) {
	issuer, roots := testIssuer(t)
	serverID := "c727d690-34d4-4b44-bd13-a132f89c5919"
	issuedAt := time.Date(2026, time.August, 13, 3, 0, 0, 0, time.UTC)
	issued, err := issuer.Issue(serverID, issuedAt)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(issued.CertificatePEM)
	if block == nil {
		t.Fatal("issued certificate PEM is invalid")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if certificate.Subject.CommonName != "lore-server-"+serverID {
		t.Fatalf("certificate CommonName = %q", certificate.Subject.CommonName)
	}
	if !certificate.NotAfter.Equal(issuedAt.Add(Lifetime)) || !issued.ExpiresAt.Equal(certificate.NotAfter) {
		t.Fatalf("certificate expiry = %v, response expiry = %v", certificate.NotAfter, issued.ExpiresAt)
	}
	if len(certificate.ExtKeyUsage) != 1 || certificate.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Fatalf("certificate extended key usage = %v", certificate.ExtKeyUsage)
	}
	if _, err := certificate.Verify(x509.VerifyOptions{
		Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		CurrentTime: issuedAt,
	}); err != nil {
		t.Fatalf("verify issued certificate: %v", err)
	}
	if _, err := x509.ParsePKCS8PrivateKey(mustPEMBlock(t, issued.PrivateKeyPEM).Bytes); err != nil {
		t.Fatalf("parse issued private key: %v", err)
	}
}

func TestIssueRejectsNonCanonicalServerID(t *testing.T) {
	issuer, _ := testIssuer(t)
	if _, err := issuer.Issue("C727D690-34D4-4B44-BD13-A132F89C5919", time.Now()); err == nil {
		t.Fatal("Issue accepted a non-canonical Lore server ID")
	}
}

func testIssuer(t *testing.T) (*Issuer, *x509.CertPool) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(365 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	issuer, err := NewIssuer(certificatePEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificatePEM) {
		t.Fatal("append test CA")
	}
	return issuer, roots
}

func mustPEMBlock(t *testing.T, encoded []byte) *pem.Block {
	t.Helper()
	block, _ := pem.Decode(encoded)
	if block == nil {
		t.Fatal("PEM block is missing")
	}
	return block
}
