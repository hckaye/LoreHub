package platform

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

type LoreTransportResolver struct {
	store                      *Store
	instanceTransportAuthority string
}

func NewLoreTransportResolver(
	store *Store,
	instanceTransportAuthority string,
) (*LoreTransportResolver, error) {
	if store == nil {
		return nil, errors.New("Lore server store is required")
	}
	normalizedAuthority, err := validateLoreServerURL(instanceTransportAuthority, true)
	if err != nil {
		return nil, fmt.Errorf("validate instance Lore transport authority: %w", err)
	}
	return &LoreTransportResolver{
		store:                      store,
		instanceTransportAuthority: normalizedAuthority,
	}, nil
}

func (resolver *LoreTransportResolver) ResolveTransport(
	ctx context.Context,
	repositoryURL string,
) (loreclient.ServerTransport, error) {
	publicAuthority, err := loreRepositoryAuthority(repositoryURL)
	if err != nil {
		return loreclient.ServerTransport{}, err
	}
	var serverID, publicURL string
	var instanceScope bool
	err = resolver.store.pool.QueryRow(ctx, `
		SELECT id, instance_scope, public_url
		FROM lore_servers
		WHERE lower(public_url) = lower($1) AND status = 'active' AND revoked_at IS NULL
	`, publicAuthority).Scan(&serverID, &instanceScope, &publicURL)
	if errors.Is(err, pgx.ErrNoRows) {
		parsed, _ := url.Parse(publicAuthority)
		return loreclient.ServerTransport{}, &loreclient.UnknownServerAuthorityError{Authority: parsed.Host}
	}
	if err != nil {
		return loreclient.ServerTransport{}, fmt.Errorf("resolve Lore server transport: %w", err)
	}
	transportAuthority := strings.ToLower(publicURL)
	if instanceScope {
		transportAuthority = resolver.instanceTransportAuthority
	}
	return loreclient.ServerTransport{
		Authority: transportAuthority,
		ServerID:  serverID,
	}, nil
}

func loreRepositoryAuthority(repositoryURL string) (string, error) {
	parsed, err := url.Parse(repositoryURL)
	if err != nil || parsed.Scheme != "lores" || parsed.Host == "" || parsed.Hostname() == "" {
		return "", errors.New("Lore repository URL must use lores with an authority")
	}
	return validateLoreServerURL((&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String(), true)
}

var _ loreclient.ServerResolver = (*LoreTransportResolver)(nil)
