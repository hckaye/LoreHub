package webhooks

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type hostResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type TargetPolicy struct {
	allowHTTP    bool
	allowPrivate bool
	resolver     hostResolver
	timeout      time.Duration
}

func NewTargetPolicy(allowHTTP bool, allowPrivate bool, timeout time.Duration) (*TargetPolicy, error) {
	if timeout <= 0 || timeout > 30*time.Second {
		return nil, errors.New("webhook request timeout must be between one nanosecond and 30 seconds")
	}
	if allowPrivate && !allowHTTP {
		return nil, errors.New("private webhook targets require the local HTTP profile")
	}
	return &TargetPolicy{
		allowHTTP: allowHTTP, allowPrivate: allowPrivate,
		resolver: net.DefaultResolver, timeout: timeout,
	}, nil
}

func (policy *TargetPolicy) Validate(ctx context.Context, rawURL string) (string, error) {
	parsed, err := parseTargetURL(rawURL, policy.allowHTTP)
	if err != nil {
		return "", err
	}
	if err := policy.validateHost(ctx, parsed.Hostname()); err != nil {
		return "", err
	}
	return parsed.String(), nil
}

func (policy *TargetPolicy) Client() *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           policy.dialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: policy.timeout,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS13},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   policy.timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("webhook redirects are disabled")
		},
	}
}

func (policy *TargetPolicy) dialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parse webhook target address: %w", err)
	}
	addresses, err := policy.resolveHost(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: policy.timeout, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, target := range addresses {
		connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(target.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		lastErr = dialErr
	}
	return nil, fmt.Errorf("connect to webhook target: %w", lastErr)
}

func (policy *TargetPolicy) validateHost(ctx context.Context, host string) error {
	_, err := policy.resolveHost(ctx, host)
	return err
}

func (policy *TargetPolicy) resolveHost(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		if !policy.allowPrivate {
			return nil, invalid("webhook target is not publicly routable")
		}
	}
	if address, err := netip.ParseAddr(host); err == nil {
		address = address.Unmap()
		if !policy.allowPrivate && !safePublicAddress(address) {
			return nil, invalid("webhook target is not publicly routable")
		}
		return []netip.Addr{address}, nil
	}
	addresses, err := policy.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve webhook target: %w", err)
	}
	if len(addresses) == 0 {
		return nil, invalid("webhook target has no IP address")
	}
	result := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !policy.allowPrivate && !safePublicAddress(address) {
			return nil, invalid("webhook target is not publicly routable")
		}
		result = append(result, address)
	}
	return result, nil
}

func parseTargetURL(rawURL string, allowHTTP bool) (*url.URL, error) {
	if rawURL == "" || strings.TrimSpace(rawURL) != rawURL || len(rawURL) > 2048 ||
		strings.ContainsAny(rawURL, "\x00\r\n\t") {
		return nil, invalid("webhook URL is invalid")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.Fragment != "" || parsed.Opaque != "" {
		return nil, invalid("webhook URL must be an absolute URL without credentials or a fragment")
	}
	if parsed.Scheme != "https" && !(allowHTTP && parsed.Scheme == "http") {
		return nil, invalid("webhook URL must use HTTPS")
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return nil, invalid("webhook URL port is invalid")
		}
	}
	return parsed, nil
}

var blockedPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
}

func safePublicAddress(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() ||
		address.IsUnspecified() {
		return false
	}
	for _, prefix := range blockedPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
