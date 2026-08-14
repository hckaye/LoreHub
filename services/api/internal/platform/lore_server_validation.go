package platform

import (
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func validateRegisterLoreServerInput(input RegisterLoreServerInput) (string, []byte, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > 160 || containsControl(name) ||
		len(input.CredentialDigest) != 32 || !loreServerKeyIDPattern.MatchString(input.CredentialKeyID) ||
		input.CredentialExpiresAt.IsZero() || !input.CredentialExpiresAt.After(time.Now().UTC()) ||
		input.CredentialExpiresAt.After(time.Now().UTC().Add(loreServerCredentialMaxAge)) ||
		!supportedLoreBuildVersion(input.LoreBuildVersion) ||
		!supportedHookModuleVersion(input.HookModuleVersion) {
		return "", nil, ErrInvalidInput
	}
	normalizedURL, err := validateLoreServerURL(input.PublicURL, input.AllowPrivateServers)
	if err != nil {
		return "", nil, ErrInvalidInput
	}
	health, err := encodedHealthMetadata(input.HealthMetadata, input.HookModuleVersion)
	if err != nil {
		return "", nil, err
	}
	return normalizedURL, health, nil
}

func validateLoreServerURL(value string, allowPrivate bool) (string, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\t\r\n \\") {
		return "", errors.New("Lore server URL is invalid")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "lores" || parsed.Host == "" || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.Opaque != "" || parsed.Path != "" || parsed.RawPath != "" ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" ||
		strings.ContainsAny(parsed.Host, "\x00\t\r\n /\\") {
		return "", errors.New("Lore server URL must be a fixed lores:// endpoint")
	}
	if port := parsed.Port(); port != "" {
		portNumber, conversionErr := strconv.Atoi(port)
		if conversionErr != nil || portNumber < 1 || portNumber > 65535 {
			return "", errors.New("Lore server URL port is invalid")
		}
	} else if strings.HasSuffix(parsed.Host, ":") {
		return "", errors.New("Lore server URL port is invalid")
	}
	hostname := parsed.Hostname()
	if strings.Contains(hostname, "%") {
		return "", errors.New("Lore server URL address zones are invalid")
	}
	address, addressErr := netip.ParseAddr(hostname)
	if addressErr == nil && !allowPrivate && restrictedLoreServerIP(address) {
		return "", errors.New("Lore server URL must not use a private or reserved IP address")
	}
	host := strings.ToLower(parsed.Host)
	return (&url.URL{Scheme: "lores", Host: host}).String(), nil
}

func loreServerAuthoritiesMatch(serverURL string, repositoryURL string) bool {
	server, err := url.Parse(serverURL)
	if err != nil || server.Scheme != "lores" || server.Host == "" || server.Path != "" {
		return false
	}
	repository, err := url.Parse(repositoryURL)
	if err != nil || repository.Scheme != "lores" || repository.Host == "" || repository.Path == "" {
		return false
	}
	return strings.EqualFold(server.Host, repository.Host)
}

func restrictedLoreServerIP(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsUnspecified() ||
		address.IsMulticast() {
		return true
	}
	for _, prefix := range restrictedLoreServerPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

var restrictedLoreServerPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func supportedLoreBuildVersion(value string) bool {
	major, minor, patch, ok := semanticVersion(value)
	return ok && major == 0 && minor == 8 && patch >= 6
}

func supportedHookModuleVersion(value string) bool {
	major, _, _, ok := semanticVersion(value)
	return ok && major == 1
}

func semanticVersion(value string) (int, int, int, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "v"))
	core, _, _ := strings.Cut(value, "+")
	core, _, _ = strings.Cut(core, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	parsed := [3]int{}
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return 0, 0, 0, false
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return 0, 0, 0, false
		}
		parsed[index] = value
	}
	return parsed[0], parsed[1], parsed[2], true
}

func encodedHealthMetadata(metadata map[string]any, hookModuleVersion string) ([]byte, error) {
	copy := make(map[string]any, len(metadata)+1)
	for key, value := range metadata {
		copy[key] = value
	}
	copy["hookModuleVersion"] = strings.TrimSpace(hookModuleVersion)
	encoded, err := json.Marshal(copy)
	if err != nil || len(encoded) > loreServerHealthMaxBytes {
		return nil, ErrInvalidInput
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil || object == nil {
		return nil, ErrInvalidInput
	}
	return encoded, nil
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}
