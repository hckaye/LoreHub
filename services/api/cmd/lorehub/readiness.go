package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/go-jose/go-jose/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/config"
	"github.com/lorehub/lorehub/services/api/internal/database"
	"github.com/lorehub/lorehub/services/api/internal/loreauth"
)

type serviceReadiness struct {
	database *pgxpool.Pool
	loreAuth *loreauth.Service
	settings config.Config
}

type readinessFile struct {
	name string
	path string
}

func newServiceReadiness(
	databasePool *pgxpool.Pool,
	loreAuth *loreauth.Service,
	settings config.Config,
) serviceReadiness {
	return serviceReadiness{database: databasePool, loreAuth: loreAuth, settings: settings}
}

func (readiness serviceReadiness) Ping(ctx context.Context) error {
	if readiness.database == nil {
		return errors.New("database pool is unavailable")
	}
	if err := readiness.database.Ping(ctx); err != nil {
		return fmt.Errorf("PostgreSQL is unavailable: %w", err)
	}
	if err := database.MigrationsReady(ctx, readiness.database); err != nil {
		return fmt.Errorf("database migrations are not ready: %w", err)
	}
	if readiness.loreAuth == nil || !hasPublicSigningKey(readiness.loreAuth.JWKS()) {
		return errors.New("Lore authentication JWKS is unavailable")
	}
	for _, file := range readiness.tlsFiles() {
		if err := requiredFile(file.path); err != nil {
			return fmt.Errorf("%s prerequisite is unavailable: %w", file.name, err)
		}
	}
	return nil
}

func (readiness serviceReadiness) tlsFiles() []readinessFile {
	return []readinessFile{
		{name: "Lore auth TLS certificate", path: readiness.settings.LoreAuthTLSCert},
		{name: "Lore auth TLS key", path: readiness.settings.LoreAuthTLSKey},
		{name: "policy TLS certificate", path: readiness.settings.PolicyTLSCert},
		{name: "policy TLS key", path: readiness.settings.PolicyTLSKey},
		{name: "policy client CA", path: readiness.settings.PolicyTLSClientCA},
	}
}

func requiredFile(path string) error {
	if path == "" {
		return errors.New("path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("path is not a regular file")
	}
	if info.Size() == 0 {
		return errors.New("file is empty")
	}
	return nil
}

func hasPublicSigningKey(jwks map[string]any) bool {
	keys, ok := jwks["keys"].([]jose.JSONWebKey)
	if !ok {
		return false
	}
	for index := range keys {
		key := &keys[index]
		if key.KeyID != "" && key.Use == "sig" && key.Algorithm == string(jose.RS256) &&
			key.IsPublic() && key.Valid() {
			return true
		}
	}
	return false
}
