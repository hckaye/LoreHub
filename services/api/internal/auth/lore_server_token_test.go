package auth

import "testing"

func TestLoreServerTokensUseDedicatedPrefixesAndDigests(t *testing.T) {
	secrets, err := NewSecretCodec("Lore server token test secret")
	if err != nil {
		t.Fatal(err)
	}
	registration, registrationDigest, err := NewLoreServerRegistrationToken(secrets)
	if err != nil {
		t.Fatal(err)
	}
	credential, credentialDigest, err := NewLoreServerCredential(secrets)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidLoreServerRegistrationToken(registration) || ValidLoreServerCredential(registration) {
		t.Fatalf("registration token prefix validation failed for %q", registration[:5])
	}
	if !ValidLoreServerCredential(credential) || ValidLoreServerRegistrationToken(credential) {
		t.Fatalf("credential prefix validation failed for %q", credential[:5])
	}
	if !secrets.Matches(registration, registrationDigest) || !secrets.Matches(credential, credentialDigest) {
		t.Fatal("Lore server token digest did not match the generated value")
	}
}

func TestLoreServerTokenValidationRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{
		"lhsr_short",
		"lhss_short",
		"lhsr_abcdefghijklmnopqrstuvwxyz0123456789ABCDE!",
		"lhss_abcdefghijklmnopqrstuvwxyz0123456789ABCDE!",
	} {
		if ValidLoreServerRegistrationToken(value) || ValidLoreServerCredential(value) {
			t.Fatalf("malformed Lore server token %q was accepted", value)
		}
	}
}
