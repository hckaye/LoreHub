package lore

import (
	"context"
	"errors"
	"fmt"
)

var ErrUnknownServerAuthority = errors.New("Lore server authority is not registered")

type UnknownServerAuthorityError struct {
	Authority string
}

func (err *UnknownServerAuthorityError) Error() string {
	return fmt.Sprintf("Lore server authority %q is not registered", err.Authority)
}

func (err *UnknownServerAuthorityError) Unwrap() error {
	return ErrUnknownServerAuthority
}

type ServerTransport struct {
	Authority string
	ServerID  string
}

type ServerResolver interface {
	ResolveTransport(ctx context.Context, repositoryURL string) (ServerTransport, error)
}

type directServerResolver struct{}

func (directServerResolver) ResolveTransport(
	ctx context.Context,
	repositoryURL string,
) (ServerTransport, error) {
	if err := ctx.Err(); err != nil {
		return ServerTransport{}, err
	}
	parsed, err := parseRepositoryURL(repositoryURL, true)
	if err != nil {
		return ServerTransport{}, err
	}
	return ServerTransport{
		Authority: parsed.Scheme + "://" + parsed.Authority,
		ServerID:  "direct",
	}, nil
}
