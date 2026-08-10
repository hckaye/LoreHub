// Command format_generated applies deterministic layout-only cleanup to the
// official Go bindings so the repository's line-length rule also covers them.
package main

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxLine = 120

var jsonTag = regexp.MustCompile(`,json=[^,\"]+`)
var encodingJSONTag = regexp.MustCompile(` json:\"[^\"]*\"`)

func main() {
	files, err := filepath.Glob("epic_urc/*.pb.go")
	if err != nil {
		panic(err)
	}
	for _, name := range files {
		if err := formatFile(name); err != nil {
			panic(err)
		}
	}
}

func formatFile(name string) error {
	source, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n")
	result := make([]string, 0, len(lines)+32)
	for _, line := range lines {
		line = removeRedundantJSONTags(line)
		result = append(result, wrapLine(line)...)
	}
	formatted, err := format.Source([]byte(strings.Join(result, "\n")))
	if err != nil {
		return fmt.Errorf("format %s: %w", name, err)
	}
	return os.WriteFile(name, formatted, 0o644)
}

func removeRedundantJSONTags(line string) string {
	if !strings.Contains(line, "protobuf:") {
		return line
	}
	line = jsonTag.ReplaceAllString(line, "")
	return encodingJSONTag.ReplaceAllString(line, "")
}

func wrapLine(line string) []string {
	if runeLen(line) <= maxLine {
		return []string{line}
	}
	if strings.Contains(line, "//") {
		if result := wrapComment(line); len(result) > 1 {
			return result
		}
	}
	if strings.Contains(line, "FullMethodName") {
		if result := splitStringLiteralWithLimit(line, 38); len(result) > 1 {
			return result
		}
	}
	if result := splitStringLiteral(line); len(result) > 1 {
		return result
	}
	if strings.HasPrefix(strings.TrimSpace(line), "func ") {
		if result := wrapFunctionSignature(line); len(result) > 1 {
			return result
		}
	}
	if strings.HasPrefix(strings.TrimSpace(line), "return ") {
		if result := wrapCall(line); len(result) > 1 {
			return result
		}
	}
	if strings.Contains(line, "RawDescriptor:") || strings.Contains(line, "CompressGZIP(") {
		if result := wrapCall(line); len(result) > 1 {
			return result
		}
	}
	if result := wrapInterfaceMethod(line); len(result) > 1 {
		return result
	}
	return []string{line}
}

func wrapComment(line string) []string {
	comment := strings.Index(line, "//")
	if comment < 0 {
		return []string{line}
	}
	prefix := line[:comment]
	words := strings.Fields(line[comment+2:])
	if len(words) == 0 {
		return []string{line}
	}
	indent := leadingWhitespace(prefix)
	result := make([]string, 0, 2)
	current := prefix + "//"
	for _, word := range words {
		candidate := current + " " + word
		if runeLen(candidate) > maxLine && current != prefix+"//" {
			result = append(result, current)
			current = indent + "// " + word
		} else {
			current = candidate
		}
	}
	result = append(result, current)
	return result
}

func splitStringLiteral(line string) []string {
	return splitStringLiteralWithLimit(line, 82)
}

func splitStringLiteralWithLimit(line string, contentLimit int) []string {
	start := strings.IndexByte(line, '"')
	if start < 0 {
		return []string{line}
	}
	end := start + 1
	for end < len(line) {
		if line[end] == '\\' {
			end += escapeLength(line[end:])
			continue
		}
		if line[end] == '"' {
			break
		}
		end++
	}
	if end >= len(line) || end-start < 12 {
		return []string{line}
	}
	content := line[start+1 : end]
	indent := leadingWhitespace(line)
	prefix := line[:start]
	suffix := line[end+1:]
	parts := splitStringContent(content, contentLimit)
	if len(parts) < 2 {
		return []string{line}
	}
	result := make([]string, 0, len(parts))
	for index, part := range parts {
		current := indent + "\"" + part + "\""
		if index == 0 {
			current = prefix + "\"" + part + "\""
		}
		if index < len(parts)-1 {
			current += " +"
		} else {
			current += suffix
		}
		result = append(result, current)
	}
	return result
}

func splitStringContent(content string, limit int) []string {
	parts := make([]string, 0, len(content)/limit+1)
	start := 0
	for start < len(content) {
		end := start
		for end < len(content) {
			next := end + 1
			if content[end] == '\\' {
				next = end + escapeLength(content[end:])
			}
			if next-start > limit && end > start {
				break
			}
			end = next
		}
		if end == start {
			end = start + 1
		}
		parts = append(parts, content[start:end])
		start = end
	}
	return parts
}

func escapeLength(value string) int {
	if len(value) < 2 {
		return 1
	}
	switch value[1] {
	case 'x':
		return minInt(4, len(value))
	case 'u':
		return minInt(6, len(value))
	case 'U':
		return minInt(10, len(value))
	}
	if value[1] >= '0' && value[1] <= '7' {
		length := 2
		for length < 4 && length < len(value) && value[length] >= '0' && value[length] <= '7' {
			length++
		}
		return length
	}
	return 2
}

func wrapFunctionSignature(line string) []string {
	function := strings.Index(line, "func ")
	if function < 0 {
		return []string{line}
	}
	open := function + len("func ")
	if open < len(line) && line[open] == '(' {
		receiverClose := matchingParen(line, open)
		if receiverClose < 0 {
			return []string{line}
		}
		open = receiverClose + 1
	}
	if next := strings.IndexByte(line[open:], '('); next >= 0 {
		open += next
	} else {
		return []string{line}
	}
	if open < 0 {
		return []string{line}
	}
	close := matchingParen(line, open)
	if close < 0 {
		return []string{line}
	}
	return wrapParameterList(line, open, close)
}

func wrapInterfaceMethod(line string) []string {
	if strings.Contains(line, "=") || strings.HasPrefix(strings.TrimSpace(line), "//") {
		return []string{line}
	}
	open := strings.IndexByte(line, '(')
	if open < 0 {
		return []string{line}
	}
	close := matchingParen(line, open)
	if close < 0 {
		return []string{line}
	}
	return wrapParameterList(line, open, close)
}

func wrapCall(line string) []string {
	open := callOpen(line)
	if open < 0 {
		return []string{line}
	}
	close := matchingParen(line, open)
	if close < 0 {
		return []string{line}
	}
	return wrapParameterList(line, open, close)
}

func callOpen(line string) int {
	if assertion := strings.Index(line, ")."); assertion >= 0 {
		if open := strings.IndexByte(line[assertion+2:], '('); open >= 0 {
			return assertion + 2 + open
		}
	}
	for _, marker := range []string{
		"protoimpl.X.CompressGZIP(",
		"unsafe.Slice(",
	} {
		if open := strings.Index(line, marker); open >= 0 {
			return open + len(marker) - 1
		}
	}
	return -1
}

func wrapParameterList(line string, open, close int) []string {
	params := splitParameters(line[open+1 : close])
	if len(params) == 0 {
		return []string{line}
	}
	indent := leadingWhitespace(line) + "\t"
	result := []string{line[:open+1]}
	for _, param := range params {
		result = append(result, indent+strings.TrimSpace(param)+",")
	}
	result = append(result, leadingWhitespace(line)+")"+line[close+1:])
	return result
}

func splitParameters(value string) []string {
	result := make([]string, 0, 4)
	start := 0
	depth := 0
	for index, character := range value {
		switch character {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				result = append(result, value[start:index])
				start = index + 1
			}
		}
	}
	if strings.TrimSpace(value[start:]) != "" {
		result = append(result, value[start:])
	}
	return result
}

func matchingParen(value string, open int) int {
	depth := 0
	for index := open; index < len(value); index++ {
		switch value[index] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func leadingWhitespace(value string) string {
	return value[:len(value)-len(strings.TrimLeft(value, " \t"))]
}

func runeLen(value string) int {
	return len([]rune(value))
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
