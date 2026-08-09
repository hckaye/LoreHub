package runner

import (
	"bytes"
	"strings"
	"testing"
)

func TestMaskingLogWriterRedactsSecretsAndAddMaskCommands(t *testing.T) {
	var output bytes.Buffer
	writer := newMaskingLogWriter(&output, map[string]string{"TOKEN": "initial-secret"})
	if _, err := writer.Write([]byte("before initial-secret\n::add-mask::dynamic-secret\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("after dynamic-secret and ")); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("dynamic-secret\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	contents := output.String()
	if strings.Contains(contents, "initial-secret") || strings.Contains(contents, "dynamic-secret") {
		t.Fatalf("secret was persisted in log: %q", contents)
	}
	if !strings.Contains(contents, "before ***") || !strings.Contains(contents, "::add-mask::***") ||
		!strings.Contains(contents, "after *** and ***") {
		t.Fatalf("redacted log did not preserve safe output: %q", contents)
	}
}

func TestMaskingLogWriterRedactsSplitSecret(t *testing.T) {
	var output bytes.Buffer
	writer := newMaskingLogWriter(&output, map[string]string{"TOKEN": "split-secret"})
	if _, err := writer.Write([]byte("split-")); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("secret\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "split-secret") || output.String() != "***\n" {
		t.Fatalf("split secret was not masked: %q", output.String())
	}
}
