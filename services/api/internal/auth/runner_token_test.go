package auth

import (
	"bytes"
	"testing"
)

func TestRunnerTokensUseDistinctPrefixesAndDigests(t *testing.T) {
	codec, err := NewSecretCodec("runner token test secret")
	if err != nil {
		t.Fatal(err)
	}
	registration, registrationDigest, err := NewRunnerRegistrationToken(codec)
	if err != nil {
		t.Fatal(err)
	}
	credential, credentialDigest, err := NewRunnerCredential(codec)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidRunnerRegistrationToken(registration) || ValidRunnerCredential(registration) {
		t.Fatalf("registration token prefix validation failed: %q", registration)
	}
	if !ValidRunnerCredential(credential) || ValidRunnerRegistrationToken(credential) {
		t.Fatalf("runner credential prefix validation failed: %q", credential)
	}
	if !codec.Matches(registration, registrationDigest) || !codec.Matches(credential, credentialDigest) {
		t.Fatal("runner token digests did not match their raw values")
	}
	if bytes.Equal(registrationDigest, credentialDigest) {
		t.Fatal("different runner tokens produced the same digest")
	}
}

func TestRunnerTokenValidationRejectsMalformedValues(t *testing.T) {
	codec, _ := NewSecretCodec("runner token test secret")
	if _, _, err := NewRunnerCredential(nil); err == nil {
		t.Fatal("a runner credential was generated without a secret codec")
	}
	registration, _, err := NewRunnerRegistrationToken(codec)
	if err != nil {
		t.Fatal(err)
	}
	credential, _, err := NewRunnerCredential(codec)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		"", "lhrr_short", "lhr_short", registration + "!", credential[:len(credential)-1],
	} {
		if ValidRunnerRegistrationToken(value) || ValidRunnerCredential(value) {
			t.Fatalf("malformed runner token was accepted: %q", value)
		}
	}
}
