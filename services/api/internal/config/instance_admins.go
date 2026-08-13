package config

import "strings"

func commaSeparatedUsernames(value string) []string {
	usernames := make([]string, 0)
	seen := make(map[string]struct{})
	for _, entry := range strings.Split(value, ",") {
		username := strings.ToLower(strings.TrimSpace(entry))
		if username == "" {
			continue
		}
		if _, duplicate := seen[username]; duplicate {
			continue
		}
		seen[username] = struct{}{}
		usernames = append(usernames, username)
	}
	return usernames
}
