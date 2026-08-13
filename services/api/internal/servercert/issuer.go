package servercert

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/google/uuid"
)

const Lifetime = 30 * 24 * time.Hour

type Certificate struct {
	CertificatePEM []byte
	PrivateKeyPEM  []byte
	Serial         string
	IssuedAt       time.Time
	ExpiresAt      time.Time
}

type Issuer struct {
	certificate *x509.Certificate
	privateKey  crypto.Signer
}

func NewIssuerFromFiles(certificatePath string, privateKeyPath string) (*Issuer, error) {
	certificatePEM, err := os.ReadFile(certificatePath)
	if err != nil {
		return nil, fmt.Errorf("read Lore server certificate CA: %w", err)
	}
	privateKeyPEM, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read Lore server certificate CA key: %w", err)
	}
	return NewIssuer(certificatePEM, privateKeyPEM)
}

func NewIssuer(certificatePEM []byte, privateKeyPEM []byte) (*Issuer, error) {
	certificateBlock, _ := pem.Decode(certificatePEM)
	if certificateBlock == nil || certificateBlock.Type != "CERTIFICATE" {
		return nil, errors.New("Lore server certificate CA PEM does not contain a certificate")
	}
	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse Lore server certificate CA: %w", err)
	}
	if !certificate.IsCA || certificate.KeyUsage&x509.KeyUsageCertSign == 0 {
		return nil, errors.New("Lore server certificate issuer is not a certificate authority")
	}
	privateKey, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	certificatePublicKey, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal Lore server certificate CA public key: %w", err)
	}
	privatePublicKey, err := x509.MarshalPKIXPublicKey(privateKey.Public())
	if err != nil {
		return nil, fmt.Errorf("marshal Lore server certificate CA private key public part: %w", err)
	}
	if !equalBytes(certificatePublicKey, privatePublicKey) {
		return nil, errors.New("Lore server certificate CA key does not match its certificate")
	}
	return &Issuer{certificate: certificate, privateKey: privateKey}, nil
}

func (issuer *Issuer) Issue(serverID string, issuedAt time.Time) (Certificate, error) {
	parsedID, err := uuid.Parse(serverID)
	if err != nil || parsedID.String() != serverID {
		return Certificate{}, errors.New("Lore server ID must be a canonical UUID")
	}
	if issuedAt.IsZero() {
		return Certificate{}, errors.New("certificate issue time is required")
	}
	issuedAt = issuedAt.UTC()
	expiresAt := issuedAt.Add(Lifetime)
	if !expiresAt.Before(issuer.certificate.NotAfter) {
		expiresAt = issuer.certificate.NotAfter.UTC()
	}
	if !expiresAt.After(issuedAt) {
		return Certificate{}, errors.New("Lore server certificate CA expires too soon")
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return Certificate{}, fmt.Errorf("generate Lore server certificate serial: %w", err)
	}
	if serial.Sign() == 0 {
		serial.SetInt64(1)
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Certificate{}, fmt.Errorf("generate Lore server certificate key: %w", err)
	}
	notBefore := issuedAt.Add(-5 * time.Minute)
	if notBefore.Before(issuer.certificate.NotBefore) {
		notBefore = issuer.certificate.NotBefore
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "lore-server-" + serverID},
		NotBefore:             notBefore,
		NotAfter:              expiresAt,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	certificateDER, err := x509.CreateCertificate(
		rand.Reader, template, issuer.certificate, &privateKey.PublicKey, issuer.privateKey,
	)
	if err != nil {
		return Certificate{}, fmt.Errorf("issue Lore server certificate: %w", err)
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return Certificate{}, fmt.Errorf("marshal Lore server certificate key: %w", err)
	}
	return Certificate{
		CertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}),
		PrivateKeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER}),
		Serial:         serial.Text(16),
		IssuedAt:       issuedAt,
		ExpiresAt:      expiresAt,
	}, nil
}

func parsePrivateKey(encoded []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(encoded)
	if block == nil {
		return nil, errors.New("Lore server certificate CA key PEM does not contain a private key")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if signer, ok := key.(crypto.Signer); ok {
			return signer, nil
		}
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, errors.New("Lore server certificate CA key is invalid or unsupported")
}

func equalBytes(left []byte, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}
