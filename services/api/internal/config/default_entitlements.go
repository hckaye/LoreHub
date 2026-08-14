package config

import (
	"fmt"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

// defaultOrganizationEntitlements reads the features every new organization is
// granted. Installations that run their own Lore Server and runners list them
// here; hosted installations leave the value empty and grant the features to
// each organization from the administration page.
func defaultOrganizationEntitlements(value string) ([]string, error) {
	features := make([]string, 0)
	seen := make(map[string]struct{})
	for _, entry := range strings.Split(value, ",") {
		feature := strings.ToLower(strings.TrimSpace(entry))
		if feature == "" {
			continue
		}
		if !platform.ValidEntitlementFeature(feature) {
			return nil, fmt.Errorf("LOREHUB_DEFAULT_ORGANIZATION_ENTITLEMENTS contains unknown feature %q", feature)
		}
		if _, duplicate := seen[feature]; duplicate {
			continue
		}
		seen[feature] = struct{}{}
		features = append(features, feature)
	}
	return features, nil
}
