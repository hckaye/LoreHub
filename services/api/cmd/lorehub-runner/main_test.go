package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigureRegistersAndWritesRestrictedConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/actions/runner/register" ||
			request.Header.Get("Authorization") != "Bearer registration-token" {
			t.Errorf("unexpected registration request: %s %s %q", request.Method, request.URL.Path,
				request.Header.Get("Authorization"))
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		var input map[string]any
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(`{
          "runner":{"id":"runner-1","name":"build-1","labels":["linux","self-hosted"]},
          "token":"runner-credential"
        }`))
	}))
	defer server.Close()
	directory := t.TempDir()
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"configure", "--url", server.URL, "--config-dir", directory,
		"--name", "build-1", "--labels", "SELF-HOSTED,Linux",
	}, bytes.NewBufferString("registration-token\n"), &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "config.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode=%o", info.Mode().Perm())
	}
	config, err := readConfig(directory)
	if err != nil {
		t.Fatal(err)
	}
	if config.RunnerID != "runner-1" || config.Token != "runner-credential" || config.URL != server.URL {
		t.Fatalf("unexpected config: %+v", config)
	}
}
