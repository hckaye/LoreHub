package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
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
	"github.com/lorehub/lorehub/services/api/internal/loreauth"
	epic_urc "github.com/lorehub/lorehub/services/api/internal/loreauth/epic_urc"
	"github.com/lorehub/lorehub/services/api/internal/milestones"
	"github.com/lorehub/lorehub/services/api/internal/platform"
	"github.com/lorehub/lorehub/services/api/internal/projects"
	"github.com/lorehub/lorehub/services/api/internal/releases"
	"github.com/lorehub/lorehub/services/api/internal/runner"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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

	settings, err := config.LoadFor(command)
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
	store := platform.NewStore(pool)
	keyProvider, err := newSigningKeyProvider(settings)
	if err != nil {
		return err
	}
	var lore *loreclient.SDKClient
	if settings.Environment == "local-insecure" {
		if command == "runner" {
			lore, err = loreclient.NewDevelopmentSDKClientWithEndpoint(
				settings.LoreCacheDir,
				settings.LoreInternalURL,
			)
		} else {
			lore, err = loreclient.NewDevelopmentSDKClient(settings.LoreCacheDir)
		}
	} else if command == "runner" {
		lore, err = loreclient.NewSDKClientWithEndpoints(
			settings.LoreCacheDir,
			settings.LoreAuthAuthority,
			settings.LoreInternalURL,
		)
	} else {
		lore, err = loreclient.NewSDKClientWithAuthAuthority(settings.LoreCacheDir, settings.LoreAuthAuthority)
	}
	if err != nil {
		return err
	}
	loreAuth, err := newLoreAuthService(store, keyProvider, settings)
	if err != nil {
		return err
	}
	issuer, err := loreauth.NewCredentialIssuer(loreAuth)
	if err != nil {
		return err
	}
	loreCredentials, err := loreclient.NewCredentialProviderWithIssuer(settings.Environment, issuer,
		settings.LoreAuthAuthority, settings.LoreCredentials, settings.LoreIdentity,
		settings.LoreAllowDevelopmentFallback)
	if err != nil {
		return err
	}
	if command == "runner" {
		return runRunner(rootContext, pool, lore, loreCredentials, keyProvider, settings, logger)
	}

	var authenticator auth.Authenticator
	var loginProvider auth.LoginProvider
	var secretCodec *auth.SecretCodec
	actionsStore := runner.NewStoreWithFiles(pool, settings.RunnerLogDir, settings.RunnerArtifactDir)
	actionsContext, err := runner.NewPostgresExecutionContextResolver(
		pool,
		settings.ActionsSecretKeyID,
		settings.ActionsSecretKey,
	)
	if err != nil {
		return err
	}
	actionsJobTokens, err := runner.NewPostgresJobTokenService(
		pool,
		keyProvider,
		settings.LoreAuthIssuer,
		settings.ActionsJobTokenAudience,
	)
	if err != nil {
		return err
	}
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
	collaborationStore := collab.NewStore(pool)
	handler := httpapi.New(
		store,
		lore,
		authenticator,
		newServiceReadiness(pool, loreAuth, settings),
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
		httpapi.WithActions(actionsStore),
		httpapi.WithActionsSecurity(actionsStore, actionsJobTokens),
		httpapi.WithActionsExecutionContext(actionsContext),
		httpapi.WithIdentityStore(store),
		httpapi.WithConfiguredLoginProviders(settings.IdentityProviders),
		httpapi.WithCollaboration(collaborationStore),
		httpapi.WithBranchObservations(store),
		httpapi.WithProjects(projects.NewStore(pool)),
		httpapi.WithReleases(releases.NewStore(pool)),
		httpapi.WithMilestones(milestones.NewStore(pool)),
		httpapi.WithAuthorization(store),
		httpapi.WithLoreAuth(loreAuth),
		httpapi.WithLorePublicURL(settings.LorePublicURL),
		httpapi.WithLegacyLoreIdentityAllowed(settings.AllowLegacyLoreIdentity),
		httpapi.WithLoreCredentials(loreCredentials),
		httpapi.WithLoreServiceSubjects(loreclient.ServiceSubjects{
			PublicReader:           settings.LorePublicReaderSubject,
			ActionsRunner:          settings.LoreActionsRunnerSubject,
			Observer:               settings.LoreObserverSubject,
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

	policyTLS, err := loadTLSConfig(settings.PolicyTLSCert, settings.PolicyTLSKey,
		settings.PolicyTLSClientCA, true)
	if err != nil {
		return err
	}
	authTLS, err := loadTLSConfig(settings.LoreAuthTLSCert, settings.LoreAuthTLSKey, "", false)
	if err != nil {
		return err
	}
	policyHandler := httpapi.NewInternalPolicyHandler(store, logger)
	policyServer := &http.Server{
		Addr: settings.PolicyAddress, Handler: policyHandler, TLSConfig: policyTLS,
		ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second,
	}
	authListener, err := net.Listen("tcp", settings.LoreAuthAddress)
	if err != nil {
		return fmt.Errorf("listen Lore auth gRPC: %w", err)
	}
	authListeners := []net.Listener{authListener}
	if settings.LoreAuthCompatAddress != "" && settings.LoreAuthCompatAddress != settings.LoreAuthAddress {
		compatListener, listenErr := net.Listen("tcp", settings.LoreAuthCompatAddress)
		if listenErr != nil {
			_ = authListener.Close()
			return fmt.Errorf("listen Lore auth compatibility endpoint: %w", listenErr)
		}
		authListeners = append(authListeners, compatListener)
	}
	authGRPC := grpc.NewServer(grpc.Creds(credentials.NewTLS(authTLS)))
	epic_urc.RegisterUrcAuthApiServer(authGRPC, loreAuth)
	rebac, err := loreauth.NewRebacService(loreAuth)
	if err != nil {
		closeListeners(authListeners)
		return err
	}
	epic_urc.RegisterRebacApiServer(authGRPC, rebac)
	policyListener, err := net.Listen("tcp", settings.PolicyAddress)
	if err != nil {
		closeListeners(authListeners)
		return fmt.Errorf("listen Lore policy endpoint: %w", err)
	}
	tlsPolicyListener := tls.NewListener(policyListener, policyTLS)
	branchPoller := runner.NewPoller(
		actionsStore,
		lore,
		loreCredentials,
		settings.LoreObserverSubject,
		settings.BranchPollPeriod,
		logger,
		settings.RunnerPlatformImages,
	)
	serverErrors := make(chan error, 3+len(authListeners))
	go func() {
		logger.Info("LoreHub API listening", "address", settings.HTTPAddress)
		serverErrors <- server.ListenAndServe()
	}()
	for _, listener := range authListeners {
		go func(listener net.Listener) {
			logger.Info("LoreHub UCS auth listening", "address", listener.Addr().String())
			serverErrors <- authGRPC.Serve(listener)
		}(listener)
	}
	go func() {
		logger.Info("LoreHub policy endpoint listening", "address", settings.PolicyAddress)
		serverErrors <- policyServer.Serve(tlsPolicyListener)
	}()
	go func() {
		serverErrors <- branchPoller.Run(rootContext)
	}()

	select {
	case <-rootContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), settings.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shut down HTTP server: %w", err)
		}
		if err := policyServer.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shut down policy server: %w", err)
		}
		authGRPC.GracefulStop()
		return nil
	case err := <-serverErrors:
		authGRPC.Stop()
		_ = policyServer.Shutdown(context.Background())
		_ = server.Shutdown(context.Background())
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	}
}

func closeListeners(listeners []net.Listener) {
	for _, listener := range listeners {
		_ = listener.Close()
	}
}

func loadTLSConfig(certPath string, keyPath string, clientCAPath string, requireClient bool) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load TLS certificate: %w", err)
	}
	config := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}}
	if !requireClient {
		return config, nil
	}
	caBytes, err := os.ReadFile(clientCAPath)
	if err != nil {
		return nil, fmt.Errorf("read policy client CA: %w", err)
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(caBytes) {
		return nil, errors.New("policy client CA does not contain a certificate")
	}
	config.ClientCAs = clientCAs
	config.ClientAuth = tls.RequireAndVerifyClientCert
	config.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 || state.PeerCertificates[0].Subject.CommonName != "lore-policy-hook" {
			return errors.New("policy hook client certificate is invalid")
		}
		for _, usage := range state.PeerCertificates[0].ExtKeyUsage {
			if usage == x509.ExtKeyUsageClientAuth {
				return nil
			}
		}
		return errors.New("policy hook client certificate lacks client authentication usage")
	}
	return config, nil
}

func newSigningKeyProvider(settings config.Config) (*loreauth.RSAKeyProvider, error) {
	return loreauth.NewRSAKeyProvider(
		settings.AuthSigningKeyPath,
		settings.AuthSigningKeyPEM,
		settings.AuthSigningKeyKID,
		settings.AuthPreviousKeys,
		settings.Environment != "production",
	)
}

func newLoreAuthService(
	store *platform.Store,
	keyProvider loreauth.SigningKeyProvider,
	settings config.Config,
) (*loreauth.Service, error) {
	tokenService, err := loreauth.NewTokenService(keyProvider, settings.LoreAuthIssuer,
		settings.LoreAuthAudience, settings.LoreAuthEnvironment, settings.LoreAuthIDP,
		settings.LoreAuthTokenTTL)
	if err != nil {
		return nil, err
	}
	return loreauth.NewService(store, store, tokenService, settings.LoreAuthLoginURL, settings.LoreAuthURL,
		settings.LoreAuthSessionTTL, settings.Environment == "development" || settings.Environment == "local-insecure")
}

func runRunner(
	ctx context.Context,
	pool *pgxpool.Pool,
	lore *loreclient.SDKClient,
	credentials loreclient.CredentialProvider,
	keyProvider runner.JobTokenSigningKeyProvider,
	settings config.Config,
	logger *slog.Logger,
) error {
	store := runner.NewStoreWithFiles(pool, settings.RunnerLogDir, settings.RunnerArtifactDir)
	executionResolver, err := configuredExecutionContextResolver(pool, settings)
	if err != nil {
		return err
	}
	jobTokenIssuer, err := configuredJobTokenIssuer(pool, keyProvider, settings)
	if err != nil {
		return err
	}
	credentialPrincipal := runner.CredentialPrincipal{
		Kind: "service", Subject: settings.LoreActionsRunnerSubject,
	}
	worker, err := runner.NewWorker(store, runner.WorkerConfig{
		Environment:         settings.Environment,
		LoreBinary:          settings.LoreBinary,
		CredentialPrincipal: credentialPrincipal,
		Credentials:         credentials,
		ServicePrincipal:    settings.LoreActionsRunnerSubject,
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
	err = worker.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func configuredExecutionContextResolver(
	pool *pgxpool.Pool,
	settings config.Config,
) (runner.ExecutionContextResolver, error) {
	if settings.DevActionsContextFallback &&
		(settings.Environment == "development" || settings.Environment == "local") {
		return runner.NewDevelopmentExecutionContextResolver(runner.ExecutionContext{
			OrganizationVariables: settings.DevActionsContext.OrganizationVariables,
			RepositoryVariables:   settings.DevActionsContext.RepositoryVariables,
			EnvironmentVariables:  settings.DevActionsContext.EnvironmentVariables,
			OrganizationSecrets:   settings.DevActionsContext.OrganizationSecrets,
			RepositorySecrets:     settings.DevActionsContext.RepositorySecrets,
			EnvironmentSecrets:    settings.DevActionsContext.EnvironmentSecrets,
		}), nil
	}
	return runner.NewPostgresExecutionContextResolver(
		pool,
		settings.ActionsSecretKeyID,
		settings.ActionsSecretKey,
	)
}

func configuredJobTokenIssuer(
	pool *pgxpool.Pool,
	keyProvider runner.JobTokenSigningKeyProvider,
	settings config.Config,
) (runner.JobTokenIssuer, error) {
	if settings.Environment == "development" || settings.Environment == "local" {
		if settings.DevActionsJobTokenFallback {
			return runner.NewDevelopmentJobTokenIssuer(
				settings.DevActionsJobToken,
				settings.LoreActionsRunnerSubject,
			), nil
		}
	}
	return runner.NewPostgresJobTokenService(
		pool,
		keyProvider,
		settings.LoreAuthIssuer,
		settings.ActionsJobTokenAudience,
	)
}
