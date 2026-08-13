package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientSendsBearerAndDecodesJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/dashboard" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "Bearer test-token" {
			t.Errorf("authorization = %q", authorization)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		OK bool `json:"ok"`
	}
	if err := client.GetJSON(context.Background(), "/api/v1/dashboard", &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK {
		t.Fatal("response did not decode")
	}
}

func TestClientDecodesProblemResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/problem+json")
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":{"code":"authentication_required","detail":"Authentication is required"}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "bad-token")
	if err != nil {
		t.Fatal(err)
	}
	err = client.GetJSON(context.Background(), "/api/v1/dashboard", &struct{}{})
	if err == nil {
		t.Fatal("GetJSON succeeded for problem response")
	}
	var problem *ProblemError
	if !errors.As(err, &problem) {
		t.Fatalf("error type = %T, want *ProblemError", err)
	}
	if problem.Status != http.StatusUnauthorized || problem.Code != "authentication_required" ||
		problem.Detail != "Authentication is required" {
		t.Fatalf("problem = %#v", problem)
	}
	if !strings.Contains(err.Error(), "Authentication is required") ||
		!strings.Contains(err.Error(), "authentication_required") {
		t.Fatalf("error = %q", err)
	}
}
