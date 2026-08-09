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

func TestMaskingLogWriterDoesNotFlushAcrossSecretBoundary(t *testing.T) {
	var output bytes.Buffer
	writer := newMaskingLogWriter(&output, map[string]string{"TOKEN": "boundary-secret"})
	contents := strings.Repeat("x", 4090) + "boundary-" + "secret\n"
	for index := 0; index < len(contents); index += 17 {
		end := index + 17
		if end > len(contents) {
			end = len(contents)
		}
		if _, err := writer.Write([]byte(contents[index:end])); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "boundary-secret") || !strings.Contains(output.String(), "***") {
		t.Fatalf("boundary secret leaked: %q", output.String())
	}
}

func TestMaskingLogWriterMasksMultilineCRLFSecrets(t *testing.T) {
	var output bytes.Buffer
	secret := "first\r\nsecond"
	writer := newMaskingLogWriter(&output, map[string]string{"TOKEN": secret})
	for _, chunk := range []string{"first\r", "\nsec", "ond\n"} {
		if _, err := writer.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "first") || strings.Contains(output.String(), "second") {
		t.Fatalf("multiline secret leaked: %q", output.String())
	}
}

func TestMaskingLogWriterAddMaskSpanningChunks(t *testing.T) {
	var output bytes.Buffer
	writer := newMaskingLogWriter(&output, nil)
	for _, chunk := range []string{"::add-m", "ask::dynamic-secret\nvalue=dynamic-", "secret\n"} {
		if _, err := writer.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "dynamic-secret") || !strings.Contains(output.String(), "value=***") {
		t.Fatalf("chunked add-mask was not applied: %q", output.String())
	}
}

func TestMaskingLogWriterRejectsLargeUnterminatedLineWithoutPartialOutput(t *testing.T) {
	var output bytes.Buffer
	var cancelled bool
	writer := newMaskingLogWriterWithLimit(
		&output,
		map[string]string{"TOKEN": "large-secret"},
		32,
		func(error) { cancelled = true },
	)
	if _, err := writer.Write([]byte("prefix large-secret " + strings.Repeat("x", 32))); err == nil {
		t.Fatal("large unterminated line was accepted")
	}
	if err := writer.Flush(); err == nil {
		t.Fatal("flush did not retain the line-size error")
	}
	if output.Len() != 0 || strings.Contains(output.String(), "large-secret") || !cancelled {
		t.Fatalf("partial or secret output survived the error: %q cancelled=%v", output.String(), cancelled)
	}
}
