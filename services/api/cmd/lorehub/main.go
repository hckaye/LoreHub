package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strings"
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
	var lore *loreclient.SDKClient
	var loreCredentials loreclient.CredentialProvider
	if settings.Environment == "development" || settings.Environment == "test" {
		loreCredentials, err = loreclient.NewCredentialProvider(settings.Environment, settings.LoreCredentials,
			settings.LoreIdentity, settings.LoreAllowDevelopmentFallback)
	} else {
		// The control-plane issuer is intentionally injected by the production
		// deployment boundary; no static or shared-identity fallback is wired here.
		loreCredentials, err = loreclient.NewProductionCredentialProvider(nil, settings.LoreAuthAuthority)
	}
	if err != nil {
		return err
	}
	if settings.Environment == "development" || settings.Environment == "test" {
		lore, err = loreclient.NewDevelopmentSDKClient(settings.LoreCacheDir)
	} else {
		lore, err = loreclient.NewSDKClientWithAuthAuthority(settings.LoreCacheDir, settings.LoreAuthAuthority)
	}
	if err != nil {
		return err
	}
	if command == "runner" {
		return runRunner(rootContext, pool, lore, loreCredentials, settings, logger)
	}

	var authenticator auth.Authenticator
	var loginProvider auth.LoginProvider
	var secretCodec *auth.SecretCodec
	store := platform.NewStore(pool)
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
	handler := httpapi.New(
		store,
		lore,
		authenticator,
		pool,
		settings.LoreIdentity,
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
		httpapi.WithIdentityStore(store),
		httpapi.WithConfiguredLoginProviders(settings.IdentityProviders),
		httpapi.WithCollaboration(collab.NewStore(pool)),
		httpapi.WithLoreCredentials(loreCredentials),
		httpapi.WithLoreServiceSubjects(loreclient.ServiceSubjects{
			PublicReader:           settings.LorePublicReaderSubject,
			ActionsRunner:          settings.LoreActionsRunnerSubject,
			RepositoryRegistration: settings.LoreRepositoryRegistrationSubject,
		}),
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
	credentials loreclient.CredentialProvider,
	settings config.Config,
	logger *slog.Logger,
) error {
	store := runner.NewStore(pool)
	worker, err := runner.NewWorker(store, runner.WorkerConfig{
		LoreBinary:  settings.LoreBinary,
		ActBinary:   settings.ActBinary,
		WorkDir:     settings.RunnerWorkDir,
		LogDir:      settings.RunnerLogDir,
		ArtifactDir: settings.RunnerArtifactDir,
		PollPeriod:  settings.RunnerPollPeriod,
		JobTimeout:  settings.RunnerJobTimeout,
	}, logger)
	if err != nil {
		return err
	}
	poller := runner.NewPoller(store, runnerLoreClient{
		client: lore, credentials: credentials, actionsRunnerSubject: settings.LoreActionsRunnerSubject,
	}, settings.LoreIdentity,
		settings.BranchPollPeriod, logger)
	errorsChannel := make(chan error, 2)
	go func() { errorsChannel <- poller.Run(ctx) }()
	go func() { errorsChannel <- worker.Run(ctx) }()
	err = <-errorsChannel
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

type runnerLoreClient struct {
	client               *loreclient.SDKClient
	credentials          loreclient.CredentialProvider
	actionsRunnerSubject string
}

func (adapter runnerLoreClient) Branches(
	ctx context.Context,
	repository loreclient.RepositoryRef,
	identity string,
) ([]loreclient.Branch, error) {
	partition := repository.LoreRepositoryID
	if partition == "" {
		parsed, err := url.Parse(repository.URL)
		if err != nil || parsed.Host == "" {
			return nil, errors.New("runner Lore repository URL is invalid")
		}
		partition = strings.TrimSpace(path.Base(parsed.Path))
	}
	if partition == "" || adapter.credentials == nil {
		return nil, loreclient.ErrCredentialUnavailable
	}
	repository.LoreRepositoryID = partition
	credential, err := adapter.credentials.ForRepository(ctx, loreclient.CredentialRequest{
		Principal:  loreclient.ServicePrincipal(loreclient.ServicePurposeActionsRunner, adapter.actionsRunnerSubject),
		Repository: repository,
		Partition:  repository.CanonicalPartition(),
		Scope:      loreclient.ScopeRead,
	})
	if err != nil {
		return nil, err
	}
	return adapter.client.Branches(ctx, repository, credential)
}
