package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

const testEncodedKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

type staticResolver map[string][]netip.Addr

func (resolver staticResolver) LookupNetIP(
	_ context.Context,
	_ string,
	host string,
) ([]netip.Addr, error) {
	addresses, found := resolver[host]
	if !found {
		return nil, &net.DNSError{Err: "not found", Name: host}
	}
	return addresses, nil
}

func TestSecretBoxAuthenticatesWebhookAndRepository(t *testing.T) {
	box, err := NewSecretBox("test-v1", testEncodedKey)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, nonce, keyID, err := box.Seal("webhook", "repository", "0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := box.Open("webhook", "repository", ciphertext, nonce, keyID)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plaintext)
	if string(plaintext) != "0123456789abcdef" {
		t.Fatalf("plaintext = %q", plaintext)
	}
	if _, err := box.Open("other", "repository", ciphertext, nonce, keyID); err == nil {
		t.Fatal("webhook AAD mismatch was accepted")
	}
	ciphertext[0] ^= 1
	if _, err := box.Open("webhook", "repository", ciphertext, nonce, keyID); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}
}

func TestSecretBoxRejectsLineWrappedAndWrongSizedKeys(t *testing.T) {
	if _, err := NewSecretBox("test-v1", testEncodedKey[:8]+"\n"+testEncodedKey[8:]); err == nil {
		t.Fatal("line-wrapped key was accepted")
	}
	short := base64.StdEncoding.EncodeToString([]byte("too short"))
	if _, err := NewSecretBox("test-v1", short); err == nil {
		t.Fatal("short key was accepted")
	}
}

func TestTargetPolicyRejectsPrivateAndReservedAddresses(t *testing.T) {
	policy, err := NewTargetPolicy(false, false, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	policy.resolver = staticResolver{
		"private.example": {netip.MustParseAddr("10.0.0.2")},
		"mixed.example": {
			netip.MustParseAddr("203.0.113.1"),
			netip.MustParseAddr("127.0.0.1"),
		},
		"public.example": {netip.MustParseAddr("8.8.8.8")},
		"nat64.example":  {netip.MustParseAddr("64:ff9b::7f00:1")},
	}
	for _, target := range []string{
		"https://private.example/hook",
		"https://mixed.example/hook",
		"https://127.0.0.1/hook",
		"https://[::1]/hook",
		"https://192.0.2.1/hook",
		"https://nat64.example/hook",
		"https://0.0.0.1/hook",
	} {
		if _, err := policy.Validate(context.Background(), target); !errors.Is(err, ErrInvalid) {
			t.Fatalf("target %q error = %v", target, err)
		}
	}
	if _, err := policy.Validate(context.Background(), "https://public.example/hook?source=lore"); err != nil {
		t.Fatal(err)
	}
	if _, err := policy.Validate(context.Background(), "http://public.example/hook"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("HTTP target error = %v", err)
	}
}

func TestDeliverySignsExactBodyAndDoesNotFollowRedirects(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	body := []byte(`{"event":"issue.created"}`)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		received, err := readRequestBody(request)
		if err != nil {
			t.Error(err)
		}
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(body)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if string(received) != string(body) || request.Header.Get("X-LoreHub-Signature-256") != expected {
			t.Errorf("signature or body mismatch")
		}
		if request.Header.Get("X-LoreHub-Delivery") != "delivery" ||
			request.Header.Get("X-LoreHub-Event") != "issue.created" {
			t.Errorf("delivery headers are missing")
		}
		writer.WriteHeader(http.StatusAccepted)
		_, _ = writer.Write([]byte("accepted"))
	}))
	defer server.Close()
	box, _ := NewSecretBox("test-v1", testEncodedKey)
	policy, _ := NewTargetPolicy(true, true, 2*time.Second)
	store := &Store{box: box, target: policy}
	worker := &Worker{store: store, client: policy.Client()}
	ciphertext, nonce, keyID, _ := box.Seal("webhook", "repository", secret)
	result := worker.send(context.Background(), claimedDelivery{
		ID: "delivery", WebhookID: "webhook", RepositoryID: "repository",
		URL: server.URL, Event: "issue.created", Body: body,
		Ciphertext: ciphertext, Nonce: nonce, KeyID: keyID,
	})
	if !result.Successful || result.Status == nil || *result.Status != http.StatusAccepted ||
		result.ResponseBody != "accepted" || result.FinishedAt.IsZero() || requests != 1 {
		t.Fatalf("unexpected delivery result: %#v requests=%d", result, requests)
	}

	redirectTarget := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("redirect target must not be called")
	}))
	defer redirectTarget.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Redirect(writer, &http.Request{}, redirectTarget.URL, http.StatusFound)
	}))
	defer redirect.Close()
	result = worker.send(context.Background(), claimedDelivery{
		ID: "delivery", WebhookID: "webhook", RepositoryID: "repository",
		URL: redirect.URL, Event: "issue.created", Body: body,
		Ciphertext: ciphertext, Nonce: nonce, KeyID: keyID,
	})
	if result.Successful || !strings.Contains(result.ErrorMessage, "did not accept") {
		t.Fatalf("redirect result = %#v", result)
	}
}

func readRequestBody(request *http.Request) ([]byte, error) {
	defer request.Body.Close()
	return io.ReadAll(request.Body)
}
