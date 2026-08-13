package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"time"
)

func prefixListSetting(name string) ([]netip.Prefix, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil, nil
	}
	values := strings.Split(raw, ",")
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("%s must contain comma-separated IP prefixes", name)
		}
		result = append(result, prefix.Masked())
	}
	return result, nil
}

func validateOperationalConfig(config Config, command string) error {
	if config.RateLimitWindow < time.Second || config.RateLimitWindow > time.Hour {
		return errors.New("LOREHUB_RATE_LIMIT_WINDOW must be between 1s and 1h")
	}
	if strings.ContainsAny(config.MetricsToken, "\r\n") {
		return errors.New("LOREHUB_METRICS_TOKEN must not contain a line break")
	}
	if command == "serve" && config.Environment == "production" && len(config.MetricsToken) < 32 {
		return errors.New("LOREHUB_METRICS_TOKEN must contain at least 32 characters in production")
	}
	return nil
}
