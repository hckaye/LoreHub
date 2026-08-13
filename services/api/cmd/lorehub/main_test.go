package main

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/servercert"
)

func TestValidatePolicyClientCertificateAcceptsLegacyAndPerServerIdentities(t *testing.T) {
	serverID := "c727d690-34d4-4b44-bd13-a132f89c5919"
	for _, commonName := range []string{servercert.LegacyCommonName, "lore-server-" + serverID} {
		certificate := &x509.Certificate{
			Subject:     pkix.Name{CommonName: commonName},
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		}
		if err := validatePolicyClientCertificate(certificate); err != nil {
			t.Errorf("certificate %q was rejected: %v", commonName, err)
		}
	}
}

func TestValidatePolicyClientCertificateRejectsUnknownIdentityAndUsage(t *testing.T) {
	for _, certificate := range []*x509.Certificate{
		{Subject: pkix.Name{CommonName: "unknown"}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}},
		{Subject: pkix.Name{CommonName: servercert.LegacyCommonName}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}},
	} {
		if err := validatePolicyClientCertificate(certificate); err == nil {
			t.Fatalf("invalid policy client certificate was accepted: %+v", certificate)
		}
	}
}
