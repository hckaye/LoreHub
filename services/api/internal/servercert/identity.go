package servercert

import (
	"strings"

	"github.com/google/uuid"
)

const (
	LegacyCommonName = "lore-policy-hook"
	commonNamePrefix = "lore-server-"
)

func ServerIDFromCommonName(commonName string) (string, bool) {
	serverID := strings.TrimPrefix(commonName, commonNamePrefix)
	if serverID == commonName {
		return "", false
	}
	parsed, err := uuid.Parse(serverID)
	if err != nil || parsed.String() != serverID {
		return "", false
	}
	return serverID, true
}

func ValidCommonName(commonName string) bool {
	if commonName == LegacyCommonName {
		return true
	}
	_, ok := ServerIDFromCommonName(commonName)
	return ok
}
