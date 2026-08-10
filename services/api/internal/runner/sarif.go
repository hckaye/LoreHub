package runner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	MaxSARIFUploadBytes = 10 << 20
	MaxSARIFRuns        = 100
	MaxSARIFResults     = 50_000

	maxSARIFJSONDepth       = 64
	maxSARIFJSONNodes       = 1_000_000
	maxSARIFStringBytes     = 1 << 20
	maxSARIFToolBytes       = 255
	maxSARIFRuleIDBytes     = 512
	maxSARIFMessageBytes    = 32 << 10
	maxSARIFPathBytes       = 1024
	maxSARIFResultLocations = 100
	maxSARIFStartLine       = 100_000_000
)

var (
	ErrSARIFInvalid   = errors.New("SARIF document is invalid")
	ErrSARIFTooLarge  = errors.New("SARIF document exceeds its size limit")
	ErrSARIFBoundary  = errors.New("SARIF job boundary does not match")
	ErrSARIFNotFound  = errors.New("SARIF upload was not found")
	ErrSARIFListLimit = errors.New("SARIF list limit is invalid")
)

type sarifDocument struct {
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name string `json:"name"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifMessage struct {
	Text     string `json:"text"`
	Markdown string `json:"markdown"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

type parsedSARIF struct {
	Version string
	Tools   []string
	Alerts  []parsedSARIFAlert
}

type parsedSARIFAlert struct {
	ToolName  string
	RuleID    string
	Level     string
	Message   string
	Path      string
	StartLine *int
}

func parseSARIF(document []byte) (parsedSARIF, error) {
	if len(document) == 0 {
		return parsedSARIF{}, fmt.Errorf("%w: document is empty", ErrSARIFInvalid)
	}
	if len(document) > MaxSARIFUploadBytes {
		return parsedSARIF{}, ErrSARIFTooLarge
	}
	if !utf8.Valid(document) {
		return parsedSARIF{}, fmt.Errorf("%w: document is not UTF-8", ErrSARIFInvalid)
	}
	if err := validateSARIFJSONShape(document); err != nil {
		return parsedSARIF{}, err
	}

	decoder := json.NewDecoder(bytes.NewReader(document))
	var decoded sarifDocument
	if err := decoder.Decode(&decoded); err != nil {
		return parsedSARIF{}, fmt.Errorf("%w: %v", ErrSARIFInvalid, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return parsedSARIF{}, err
	}
	if decoded.Version != "2.1.0" {
		return parsedSARIF{}, fmt.Errorf("%w: version must be 2.1.0", ErrSARIFInvalid)
	}
	if len(decoded.Runs) == 0 || len(decoded.Runs) > MaxSARIFRuns {
		return parsedSARIF{}, fmt.Errorf("%w: runs must contain between 1 and %d entries",
			ErrSARIFInvalid, MaxSARIFRuns)
	}

	parsed := parsedSARIF{Version: decoded.Version, Tools: make([]string, 0, len(decoded.Runs))}
	for _, run := range decoded.Runs {
		toolName, err := validateSARIFText("tool name", run.Tool.Driver.Name, maxSARIFToolBytes, false)
		if err != nil {
			return parsedSARIF{}, err
		}
		parsed.Tools = append(parsed.Tools, toolName)
		if len(parsed.Alerts)+len(run.Results) > MaxSARIFResults {
			return parsedSARIF{}, fmt.Errorf("%w: results exceed %d entries", ErrSARIFInvalid, MaxSARIFResults)
		}
		for _, result := range run.Results {
			alert, err := parseSARIFResult(toolName, result)
			if err != nil {
				return parsedSARIF{}, err
			}
			parsed.Alerts = append(parsed.Alerts, alert)
		}
	}
	return parsed, nil
}

func parseSARIFResult(toolName string, result sarifResult) (parsedSARIFAlert, error) {
	ruleID, err := validateSARIFText("ruleId", result.RuleID, maxSARIFRuleIDBytes, false)
	if err != nil {
		return parsedSARIFAlert{}, err
	}
	level := result.Level
	if level == "" {
		level = "warning"
	}
	switch level {
	case "none", "note", "warning", "error":
	default:
		return parsedSARIFAlert{}, fmt.Errorf("%w: result level is invalid", ErrSARIFInvalid)
	}
	message := result.Message.Text
	if message == "" {
		message = result.Message.Markdown
	}
	message, err = validateSARIFText("result message", message, maxSARIFMessageBytes, true)
	if err != nil {
		return parsedSARIFAlert{}, err
	}
	if len(result.Locations) == 0 || len(result.Locations) > maxSARIFResultLocations {
		return parsedSARIFAlert{}, fmt.Errorf("%w: result locations are missing or exceed %d entries",
			ErrSARIFInvalid, maxSARIFResultLocations)
	}
	var alertPath string
	var startLine *int
	for index, location := range result.Locations {
		candidate, pathErr := validateSARIFPath(location.PhysicalLocation.ArtifactLocation)
		if pathErr != nil {
			return parsedSARIFAlert{}, pathErr
		}
		line := location.PhysicalLocation.Region.StartLine
		if line < 0 || line > maxSARIFStartLine {
			return parsedSARIFAlert{}, fmt.Errorf("%w: startLine is outside its bound", ErrSARIFInvalid)
		}
		if index == 0 {
			alertPath = candidate
			if line > 0 {
				startLine = &line
			}
		}
	}
	return parsedSARIFAlert{
		ToolName:  toolName,
		RuleID:    ruleID,
		Level:     level,
		Message:   message,
		Path:      alertPath,
		StartLine: startLine,
	}, nil
}

func validateSARIFPath(location sarifArtifactLocation) (string, error) {
	if location.URIBaseID != "" && location.URIBaseID != "%SRCROOT%" {
		return "", fmt.Errorf("%w: uriBaseId must be %%SRCROOT%%", ErrSARIFInvalid)
	}
	if location.URI == "" || len(location.URI) > maxSARIFPathBytes*3 ||
		strings.ContainsAny(location.URI, "\\\x00\r\n\t") {
		return "", fmt.Errorf("%w: result path is invalid", ErrSARIFInvalid)
	}
	parsed, err := url.Parse(location.URI)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", fmt.Errorf("%w: result path must be repository-relative", ErrSARIFInvalid)
	}
	candidate := parsed.Path
	if candidate == "" || len(candidate) > maxSARIFPathBytes || strings.HasPrefix(candidate, "/") ||
		strings.ContainsAny(candidate, "\\\x00\r\n\t") {
		return "", fmt.Errorf("%w: result path must be repository-relative", ErrSARIFInvalid)
	}
	segments := strings.Split(candidate, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("%w: result path contains traversal", ErrSARIFInvalid)
		}
	}
	if cleaned := path.Clean(candidate); cleaned != candidate || cleaned == "." {
		return "", fmt.Errorf("%w: result path is not canonical", ErrSARIFInvalid)
	}
	return candidate, nil
}

func validateSARIFText(label string, value string, maxBytes int, allowNewlines bool) (string, error) {
	if value == "" || len(value) > maxBytes || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("%w: %s is missing or too long", ErrSARIFInvalid, label)
	}
	if !allowNewlines && strings.ContainsAny(value, "\r\n\t") {
		return "", fmt.Errorf("%w: %s contains control characters", ErrSARIFInvalid, label)
	}
	return value, nil
}

func validateSARIFJSONShape(document []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()
	nodes := 0
	if err := inspectSARIFJSONToken(decoder, 1, &nodes); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%w: document contains multiple JSON values", ErrSARIFInvalid)
		}
		return fmt.Errorf("%w: %v", ErrSARIFInvalid, err)
	}
	return nil
}

func inspectSARIFJSONToken(decoder *json.Decoder, depth int, nodes *int) error {
	*nodes++
	if *nodes > maxSARIFJSONNodes {
		return fmt.Errorf("%w: JSON value count exceeds its bound", ErrSARIFInvalid)
	}
	if depth > maxSARIFJSONDepth {
		return fmt.Errorf("%w: JSON nesting exceeds its bound", ErrSARIFInvalid)
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSARIFInvalid, err)
	}
	switch typed := token.(type) {
	case string:
		if len(typed) > maxSARIFStringBytes {
			return fmt.Errorf("%w: JSON string exceeds its bound", ErrSARIFInvalid)
		}
	case json.Delim:
		switch typed {
		case '{':
			keys := make(map[string]struct{})
			for decoder.More() {
				keyToken, keyErr := decoder.Token()
				if keyErr != nil {
					return fmt.Errorf("%w: %v", ErrSARIFInvalid, keyErr)
				}
				key, ok := keyToken.(string)
				if !ok || len(key) > maxSARIFStringBytes {
					return fmt.Errorf("%w: JSON object key is invalid", ErrSARIFInvalid)
				}
				*nodes++
				if *nodes > maxSARIFJSONNodes {
					return fmt.Errorf("%w: JSON value count exceeds its bound", ErrSARIFInvalid)
				}
				if _, duplicate := keys[key]; duplicate {
					return fmt.Errorf("%w: duplicate JSON object key %q", ErrSARIFInvalid, key)
				}
				keys[key] = struct{}{}
				if err := inspectSARIFJSONToken(decoder, depth+1, nodes); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := inspectSARIFJSONToken(decoder, depth+1, nodes); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("%w: unexpected JSON delimiter", ErrSARIFInvalid)
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil {
			return fmt.Errorf("%w: %v", ErrSARIFInvalid, closeErr)
		}
		if closeDelim, ok := closing.(json.Delim); !ok ||
			(typed == '{' && closeDelim != '}') || (typed == '[' && closeDelim != ']') {
			return fmt.Errorf("%w: mismatched JSON delimiter", ErrSARIFInvalid)
		}
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%w: document contains multiple JSON values", ErrSARIFInvalid)
		}
		return fmt.Errorf("%w: %v", ErrSARIFInvalid, err)
	}
	return nil
}
