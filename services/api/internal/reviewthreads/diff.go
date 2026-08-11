package reviewthreads

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

const (
	maxDiffFiles      = 2
	maxDiffPatchBytes = 4 << 20
)

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

func lineFromDiff(diff loreclient.Diff, path string, side Side, lineNumber int) (string, error) {
	if diff.HasMore || diff.Truncated {
		return "", invalid("the Lore diff is too large to anchor a review comment")
	}
	for _, file := range diff.Files {
		if file.Path != path {
			continue
		}
		if file.Binary || file.Truncated || file.Patch == "" {
			return "", invalid("review comments require a complete text diff")
		}
		line, ok := patchLine(file.Patch, side, lineNumber)
		if !ok {
			return "", invalid("the selected line is not present in the current diff")
		}
		if len(line) > 8192 {
			return "", invalid("the selected line is too long to review")
		}
		return line, nil
	}
	return "", invalid("the selected path is not present in the current diff")
}

func patchLine(patch string, side Side, requested int) (string, bool) {
	if requested < 1 || (side != SideLeft && side != SideRight) {
		return "", false
	}
	oldLine, newLine := 0, 0
	inHunk := false
	for _, line := range strings.Split(patch, "\n") {
		if match := hunkHeader.FindStringSubmatch(line); match != nil {
			oldLine, _ = strconv.Atoi(match[1])
			newLine, _ = strconv.Atoi(match[2])
			inHunk = true
			continue
		}
		if !inHunk || line == `\ No newline at end of file` || line == "" {
			continue
		}
		switch line[0] {
		case ' ':
			if (side == SideLeft && oldLine == requested) || (side == SideRight && newLine == requested) {
				return line[1:], true
			}
			oldLine++
			newLine++
		case '-':
			if side == SideLeft && oldLine == requested {
				return line[1:], true
			}
			oldLine++
		case '+':
			if side == SideRight && newLine == requested {
				return line[1:], true
			}
			newLine++
		}
	}
	return "", false
}

func normalizeCreate(input CreateInput) (CreateInput, error) {
	input.Path = strings.TrimSpace(input.Path)
	input.ExpectedBaseRevision = strings.TrimSpace(input.ExpectedBaseRevision)
	input.ExpectedHeadRevision = strings.TrimSpace(input.ExpectedHeadRevision)
	if input.Path == "" || len(input.Path) > 4096 || strings.HasPrefix(input.Path, "/") ||
		strings.Contains(input.Path, "\x00") || hasParentSegment(input.Path) {
		return CreateInput{}, invalid("review path is invalid")
	}
	if input.Side != SideLeft && input.Side != SideRight {
		return CreateInput{}, invalid("review side must be left or right")
	}
	if input.LineNumber < 1 {
		return CreateInput{}, invalid("review line must be positive")
	}
	if input.ExpectedBaseRevision == "" || input.ExpectedHeadRevision == "" ||
		len(input.ExpectedBaseRevision) > 512 || len(input.ExpectedHeadRevision) > 512 {
		return CreateInput{}, invalid("current base and head revisions are required")
	}
	body, err := normalizeBody(input.Body)
	if err != nil {
		return CreateInput{}, err
	}
	input.Body = body
	return input, nil
}

func hasParentSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." || segment == "." || segment == "" {
			return true
		}
	}
	return false
}

func validateVersion(version int) error {
	if version < 1 {
		return invalid("expected version must be positive")
	}
	return nil
}

func validateID(name string, value string) error {
	if _, err := uuid.Parse(strings.TrimSpace(value)); err != nil {
		return invalid(name + " is invalid")
	}
	return nil
}
