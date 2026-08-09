package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	"github.com/lorehub/lorehub/services/api/internal/config"
	"github.com/lorehub/lorehub/services/api/internal/database"
	"github.com/lorehub/lorehub/services/api/internal/httpapi"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
	"github.com/lorehub/lorehub/services/api/internal/runner"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("LoreHub stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if command != "serve" && command != "migrate" && command != "runner" {
		return fmt.Errorf("unknown command %q: expected serve, migrate, or runner", command)
	}

	settings, err := config.Load()
	if err != nil {
		return err
	}
	rootContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.Open(rootContext, settings.DatabaseURL, settings.DatabaseTimeout)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := database.Migrate(rootContext, pool); err != nil {
		return err
	}
	if command == "migrate" {
		logger.Info("PostgreSQL migrations are current")
		return nil
	}
	lore, err := loreclient.NewSDKClient(settings.LoreCacheDir)
	if err != nil {
		return err
	}
	if command == "runner" {
		return runRunner(rootContext, pool, lore, settings, logger)
	}

	var authenticator auth.Authenticator
	var loginProvider auth.LoginProvider
	var secretCodec *auth.SecretCodec
	store := platform.NewStore(pool)
	actionsStore := runner.NewStoreWithFiles(pool, settings.RunnerLogDir, settings.RunnerArtifactDir)
	switch settings.AuthMode {
	case config.AuthModeInteractive:
		provider, err := auth.NewOIDCProvider(rootContext, auth.OIDCConfig{
			Issuer:       settings.OIDCIssuer,
			ClientID:     settings.OIDCClientID,
			Audience:     settings.OIDCAudience,
			ClientSecret: settings.OIDCClientSecret,
			RedirectURL:  settings.OIDCRedirectURL,
		})
		if err != nil {
			return err
		}
		secretCodec, err = auth.NewSecretCodec(settings.AuthSecret)
		if err != nil {
			return err
		}
		authenticator = provider
		loginProvider = provider
	case config.AuthModeBearer:
		authenticator, err = auth.NewOIDC(rootContext, settings.OIDCIssuer, settings.OIDCAudience)
		if err != nil {
			return err
		}
	case config.AuthModeDisabled:
		authenticator = auth.DisabledAuthenticator{}
	default:
		return fmt.Errorf("unsupported authentication mode %q", settings.AuthMode)
	}
	loreIdentity := ""
	if settings.DevLoreIdentityFallback && (settings.Environment == "development" || settings.Environment == "local") {
		loreIdentity = settings.DevLoreIdentity
	}
	handler := httpapi.New(
		store,
		lore,
		authenticator,
		pool,
		loreIdentity,
		logger,
		httpapi.WithAuthentication(httpapi.AuthOptions{
			LoginProvider:  loginProvider,
			LoginStore:     store,
			SessionStore:   store,
			CleanupStore:   store,
			Secrets:        secretCodec,
			PublicOrigin:   settings.PublicOrigin,
			SessionTTL:     settings.SessionTTL,
			TransactionTTL: settings.LoginTransactionTTL,
			SessionCookie: httpapi.SessionCookieOptions{
				Name:             settings.SessionCookieName,
				LoginBindingName: settings.LoginBindingCookieName,
				Path:             settings.SessionCookiePath,
				Domain:           settings.SessionCookieDomain,
				Secure:           settings.SessionCookieSecure,
			},
		}),
		httpapi.WithActions(actionsStore),
		httpapi.WithCollaboration(collab.NewStore(pool)),
	)
	server := &http.Server{
		Addr:              settings.HTTPAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("LoreHub API listening", "address", settings.HTTPAddress)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-rootContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), settings.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shut down HTTP server: %w", err)
		}
		return nil
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	}
}

func runRunner(
	ctx context.Context,
	pool *pgxpool.Pool,
	lore *loreclient.SDKClient,
	settings config.Config,
	logger *slog.Logger,
) error {
	store := runner.NewStoreWithFiles(pool, settings.RunnerLogDir, settings.RunnerArtifactDir)
	credentialIssuer, err := configuredLoreCredentialIssuer(settings)
	if err != nil {
		return err
	}
	executionResolver := configuredExecutionContextResolver(settings)
	jobTokenIssuer, err := configuredJobTokenIssuer(settings)
	if err != nil {
		return err
	}
	credentialPrincipal := runner.CredentialPrincipal{Kind: "service", Subject: settings.RunnerSubject}
	worker, err := runner.NewWorker(store, runner.WorkerConfig{
		Environment:         settings.Environment,
		LoreBinary:          settings.LoreBinary,
		CredentialIssuer:    credentialIssuer,
		CredentialPrincipal: credentialPrincipal,
		JobTokenIssuer:      jobTokenIssuer,
		ExecutionResolver:   executionResolver,
		GitHubContext: runner.GitHubContext{
			ServerURL: settings.PublicOrigin, APIURL: settings.PublicAPIURL,
			GraphQLURL: settings.PublicGraphQLURL,
		},
		ActionSourceURL:       settings.ActionSourceURL,
		PlatformImages:        settings.RunnerPlatformImages,
		RevisionClient:        lore,
		ActBinary:             settings.ActBinary,
		WorkDir:               settings.RunnerWorkDir,
		LogDir:                settings.RunnerLogDir,
		ArtifactDir:           settings.RunnerArtifactDir,
		PollPeriod:            settings.RunnerPollPeriod,
		JobTimeout:            settings.RunnerJobTimeout,
		LeaseDuration:         settings.RunnerLeaseDuration,
		LogMaxBytes:           settings.RunnerLogMaxBytes,
		LogMaxLineBytes:       settings.RunnerLogMaxLineBytes,
		ArtifactMaxCount:      settings.RunnerArtifactMaxCount,
		ArtifactMaxFileBytes:  settings.RunnerArtifactMaxFile,
		ArtifactMaxTotalBytes: settings.RunnerArtifactMaxTotal,
		ProxyURL:              settings.RunnerProxyURL,
		EngineProxyURL:        settings.RunnerEngineProxyURL,
	}, logger)
	if err != nil {
		return err
	}
	poller := runner.NewPoller(
		store,
		lore,
		credentialIssuer,
		credentialPrincipal,
		settings.BranchPollPeriod,
		logger,
		settings.RunnerPlatformImages,
	)
	errorsChannel := make(chan error, 2)
	go func() { errorsChannel <- poller.Run(ctx) }()
	go func() { errorsChannel <- worker.Run(ctx) }()
	err = <-errorsChannel
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func configuredExecutionContextResolver(settings config.Config) runner.ExecutionContextResolver {
	if settings.DevActionsContextFallback &&
		(settings.Environment == "development" || settings.Environment == "local") {
		return runner.NewDevelopmentExecutionContextResolver(runner.ExecutionContext{
			OrganizationVariables: settings.DevActionsContext.OrganizationVariables,
			RepositoryVariables:   settings.DevActionsContext.RepositoryVariables,
			EnvironmentVariables:  settings.DevActionsContext.EnvironmentVariables,
			OrganizationSecrets:   settings.DevActionsContext.OrganizationSecrets,
			RepositorySecrets:     settings.DevActionsContext.RepositorySecrets,
			EnvironmentSecrets:    settings.DevActionsContext.EnvironmentSecrets,
		})
	}
	return runner.NewFailClosedExecutionContextResolver()
}

func configuredLoreCredentialIssuer(settings config.Config) (runner.CredentialIssuer, error) {
	if settings.Environment == "development" || settings.Environment == "local" {
		if settings.DevLoreIdentityFallback {
			return runner.NewDevelopmentCredentialIssuer(settings.DevLoreIdentity), nil
		}
	}
	return nil, fmt.Errorf("an injected repository-scoped Lore credential issuer is required for %s", settings.Environment)
}

func configuredJobTokenIssuer(settings config.Config) (runner.JobTokenIssuer, error) {
	if settings.Environment == "development" || settings.Environment == "local" {
		if settings.DevActionsJobTokenFallback {
			return runner.NewDevelopmentJobTokenIssuer(settings.DevActionsJobToken, settings.RunnerSubject), nil
		}
	}
	return nil, fmt.Errorf("an injected Actions job token issuer is required for %s", settings.Environment)
}
