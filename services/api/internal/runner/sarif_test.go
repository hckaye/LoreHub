package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestParseSARIFExtractsAlertsFromStandardCodeQLDocument(t *testing.T) {
	document := []byte(`{
		"$schema":"https://json.schemastore.org/sarif-2.1.0.json",
		"version":"2.1.0",
		"runs":[{
			"tool":{"driver":{"name":"CodeQL","semanticVersion":"2.20.0"}},
			"results":[{
				"ruleId":"js/path-injection",
				"level":"error",
				"message":{"text":"Unsanitized path input"},
				"locations":[{"physicalLocation":{
					"artifactLocation":{"uri":"src/server.ts","uriBaseId":"%SRCROOT%"},
					"region":{"startLine":42,"startColumn":7}
				}}]
			}]
		}]
	}`)
	parsed, err := parseSARIF(document)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Version != "2.1.0" || len(parsed.Tools) != 1 || parsed.Tools[0] != "CodeQL" ||
		len(parsed.Alerts) != 1 {
		t.Fatalf("unexpected parsed metadata: %#v", parsed)
	}
	alert := parsed.Alerts[0]
	if alert.RuleID != "js/path-injection" || alert.Level != "error" || alert.Path != "src/server.ts" ||
		alert.StartLine == nil || *alert.StartLine != 42 || alert.Message != "Unsanitized path input" {
		t.Fatalf("unexpected parsed alert: %#v", alert)
	}
}

func TestParseSARIFRejectsOversizeMalformedAndUnsafeShapes(t *testing.T) {
	tooManyRuns := make([]map[string]any, MaxSARIFRuns+1)
	for index := range tooManyRuns {
		tooManyRuns[index] = map[string]any{"tool": map[string]any{"driver": map[string]any{"name": "tool"}}}
	}
	tooManyRunsDocument, err := json.Marshal(map[string]any{"version": "2.1.0", "runs": tooManyRuns})
	if err != nil {
		t.Fatal(err)
	}
	tooManyResults := make([]map[string]any, MaxSARIFResults+1)
	tooManyResultsDocument, err := json.Marshal(map[string]any{
		"version": "2.1.0",
		"runs": []any{map[string]any{
			"tool":    map[string]any{"driver": map[string]any{"name": "tool"}},
			"results": tooManyResults,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	deepDocument := []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"tool"}}},` +
		`{"properties":` + strings.Repeat(`[`, maxSARIFJSONDepth+1) + `null` +
		strings.Repeat(`]`, maxSARIFJSONDepth+1) + `}]}`)
	longStringDocument := []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"tool"}},` +
		`"properties":{"large":"` + strings.Repeat("a", maxSARIFStringBytes+1) + `"}}]}`)

	testCases := []struct {
		name     string
		document []byte
		want     error
	}{
		{name: "oversize", document: make([]byte, MaxSARIFUploadBytes+1), want: ErrSARIFTooLarge},
		{name: "malformed", document: []byte(`{"version":`), want: ErrSARIFInvalid},
		{name: "duplicate key", document: []byte(`{"version":"2.1.0","version":"2.1.0","runs":[]}`),
			want: ErrSARIFInvalid},
		{name: "trailing", document: append(validSARIFDocument("src/main.go"), []byte(` {}`)...),
			want: ErrSARIFInvalid},
		{name: "version", document: []byte(`{"version":"2.0.0","runs":[]}`), want: ErrSARIFInvalid},
		{name: "too many runs", document: tooManyRunsDocument, want: ErrSARIFInvalid},
		{name: "too many results", document: tooManyResultsDocument, want: ErrSARIFInvalid},
		{name: "deep nesting", document: deepDocument, want: ErrSARIFInvalid},
		{name: "long string", document: longStringDocument, want: ErrSARIFInvalid},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, parseErr := parseSARIF(testCase.document)
			if !errors.Is(parseErr, testCase.want) {
				t.Fatalf("parse error = %v, want %v", parseErr, testCase.want)
			}
		})
	}
}

func TestParseSARIFRejectsPathsOutsideRepository(t *testing.T) {
	testCases := []struct {
		name      string
		path      string
		uriBaseID string
	}{
		{name: "parent", path: "../secret.txt"},
		{name: "encoded parent", path: "%2e%2e/secret.txt"},
		{name: "absolute", path: "/etc/passwd"},
		{name: "file URI", path: "file:///etc/passwd"},
		{name: "windows", path: `C:\\Windows\\system.ini`},
		{name: "non canonical", path: "src/./main.go"},
		{name: "unsupported base", path: "src/main.go", uriBaseID: "%PROJECTROOT%"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			document := validSARIFDocumentWithBase(testCase.path, testCase.uriBaseID)
			if _, err := parseSARIF(document); !errors.Is(err, ErrSARIFInvalid) {
				t.Fatalf("unsafe path was accepted: %v", err)
			}
		})
	}
}

func TestValidateSARIFExpectedRevisionAndRef(t *testing.T) {
	if err := validateSARIFExpectedRevisionAndRef("lore-revision-1", "refs/heads/main"); err != nil {
		t.Fatalf("valid revision and ref were rejected: %v", err)
	}
	testCases := []struct {
		name     string
		revision string
		ref      string
	}{
		{name: "missing revision", ref: "refs/heads/main"},
		{name: "missing ref", revision: "lore-revision-1"},
		{name: "tag ref", revision: "lore-revision-1", ref: "refs/tags/v1"},
		{name: "traversal ref", revision: "lore-revision-1", ref: "refs/heads/release/../main"},
		{name: "whitespace revision", revision: "lore revision", ref: "refs/heads/main"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateSARIFExpectedRevisionAndRef(testCase.revision, testCase.ref)
			if !errors.Is(err, ErrSARIFBoundary) {
				t.Fatalf("validation error = %v, want %v", err, ErrSARIFBoundary)
			}
		})
	}
}

func validSARIFDocument(filePath string) []byte {
	return validSARIFDocumentWithBase(filePath, "")
}

func validSARIFDocumentWithBase(filePath string, uriBaseID string) []byte {
	base := ""
	if uriBaseID != "" {
		base = fmt.Sprintf(`,"uriBaseId":%q`, uriBaseID)
	}
	return []byte(fmt.Sprintf(`{
		"version":"2.1.0",
		"runs":[{
			"tool":{"driver":{"name":"CodeQL"}},
			"results":[{
				"ruleId":"go/sql-injection",
				"level":"warning",
				"message":{"text":"Unsafe query construction"},
				"locations":[{"physicalLocation":{
					"artifactLocation":{"uri":%q%s},
					"region":{"startLine":12}
				}}]
			}]
		}]
	}`, filePath, base))
}
