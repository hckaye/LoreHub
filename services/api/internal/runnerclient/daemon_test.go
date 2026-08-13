package runnerclient

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/runner"
)

type fakeExecutor struct{}

func (fakeExecutor) Execute(context.Context, runner.Job, *Client) (ExecutionResult, error) {
	return ExecutionResult{
		Conclusion: "success",
		Log:        []byte("fake execution passed\n"),
		Artifacts:  []Artifact{{Name: "result.txt", Content: []byte("result")}},
	}, nil
}

func TestDaemonClaimsExecutesUploadsAndCompletes(t *testing.T) {
	var mutex sync.Mutex
	claimed := false
	var logBody, artifactBody []byte
	completed := ""
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer runner-credential" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		mutex.Lock()
		defer mutex.Unlock()
		switch request.URL.Path {
		case "/api/v1/actions/runner/claim":
			if claimed {
				writer.WriteHeader(http.StatusNoContent)
				return
			}
			claimed = true
			writeTestJSON(writer, map[string]any{
				"job": runner.Job{ID: "job-1", RunID: "run-1", Attempt: 1}, "leaseSeconds": 60,
			})
		case "/api/v1/actions/runner/jobs/job-1/logs":
			logBody, _ = io.ReadAll(request.Body)
			writeTestJSON(writer, map[string]int{"size": len(logBody)})
		case "/api/v1/actions/runner/jobs/job-1/artifacts":
			if request.Header.Get("X-LoreHub-Artifact-Name") != "result.txt" ||
				request.Header.Get("X-LoreHub-Upload-Complete") != "true" {
				t.Errorf("artifact headers were not sent: %v", request.Header)
			}
			artifactBody, _ = io.ReadAll(request.Body)
			writeTestJSON(writer, map[string]int{"size": len(artifactBody)})
		case "/api/v1/actions/runner/jobs/job-1/complete":
			var input map[string]string
			_ = json.NewDecoder(request.Body).Decode(&input)
			completed = input["conclusion"]
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected daemon request: %s %s", request.Method, request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "runner-credential", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	daemon, err := NewDaemon(
		client, fakeExecutor{}, time.Millisecond, time.Minute,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	wasClaimed, err := daemon.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !wasClaimed || string(logBody) != "fake execution passed\n" ||
		string(artifactBody) != "result" || completed != "success" {
		t.Fatalf("daemon result claimed=%v log=%q artifact=%q completed=%q",
			wasClaimed, logBody, artifactBody, completed)
	}
}

func writeTestJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}
