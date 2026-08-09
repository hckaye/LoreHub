package lore

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

type parsedRepositoryURL struct {
	Scheme    string
	Authority string
	Partition string
}

func parseRepositoryURL(value string, allowPlain bool) (parsedRepositoryURL, error) {
	if value == "" || strings.TrimSpace(value) != value ||
		strings.ContainsAny(value, "\x00\t\r\n \\") {
		return parsedRepositoryURL{}, errors.New("Lore repository URL is invalid")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Opaque != "" ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" {
		return parsedRepositoryURL{}, errors.New("Lore repository URL must have no userinfo, query, or fragment")
	}
	wantedScheme := "lores"
	if allowPlain {
		if parsed.Scheme != "lores" && parsed.Scheme != "lore" {
			return parsedRepositoryURL{}, errors.New("Lore repository URL must use lores or explicit development lore")
		}
	} else if parsed.Scheme != wantedScheme {
		return parsedRepositoryURL{}, errors.New("production Lore repository URL must use lores")
	}
	if strings.ContainsAny(parsed.Host, "\x00\t\r\n /\\") || parsed.Hostname() == "" {
		return parsedRepositoryURL{}, errors.New("Lore repository URL authority is invalid")
	}
	if port := parsed.Port(); port != "" {
		value, conversionErr := strconv.Atoi(port)
		if conversionErr != nil || value < 1 || value > 65535 {
			return parsedRepositoryURL{}, errors.New("Lore repository URL port is invalid")
		}
	} else if strings.HasSuffix(parsed.Host, ":") {
		return parsedRepositoryURL{}, errors.New("Lore repository URL port is invalid")
	}
	if parsed.RawPath != "" || parsed.Path == "" || parsed.Path[0] != '/' ||
		strings.Count(parsed.Path, "/") != 1 {
		return parsedRepositoryURL{}, errors.New("Lore repository URL must contain one partition path")
	}
	partition := parsed.Path[1:]
	if !validPartitionSegment(partition) {
		return parsedRepositoryURL{}, errors.New("Lore repository URL partition path is invalid")
	}
	return parsedRepositoryURL{Scheme: parsed.Scheme, Authority: parsed.Host, Partition: partition}, nil
}

func validPartitionSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}
