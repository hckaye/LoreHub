package runner

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAppendRunnerUploadIsIdempotentForMatchingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upload")
	content := []byte("runner log")
	size, err := appendRunnerUpload(path, 0, content, 1024)
	if err != nil || size != int64(len(content)) {
		t.Fatalf("initial upload size=%d err=%v", size, err)
	}
	size, err = appendRunnerUpload(path, 0, content, 1024)
	if err != nil || size != int64(len(content)) {
		t.Fatalf("matching retry size=%d err=%v", size, err)
	}
	if _, err := appendRunnerUpload(path, 0, []byte("different!"), 1024); !errors.Is(err, ErrRunnerUploadOffset) {
		t.Fatalf("different retry was accepted: %v", err)
	}
	stored, err := os.ReadFile(path)
	if err != nil || string(stored) != string(content) {
		t.Fatalf("stored upload changed: %q, %v", stored, err)
	}
}
