package repodeletion

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const (
	maxDeletionsPerCycle = 10
	leaseSafetyMargin    = 30 * time.Second
)

type Store interface {
	ClaimRepositoryDeletion(context.Context, string, time.Duration) (*platform.RepositoryDeletionClaim, error)
	FailRepositoryDeletion(
		context.Context,
		string,
		platform.RepositoryDeletionClaim,
		time.Duration,
		error,
	) error
	CompleteRepositoryDeletion(context.Context, string, platform.RepositoryDeletionClaim) error
}

type Config struct {
	PollPeriod       time.Duration
	OperationTimeout time.Duration
	LeaseDuration    time.Duration
	ServiceSubject   string
}

type Worker struct {
	store       Store
	client      loreclient.RepositoryDeletionClient
	credentials loreclient.CredentialProvider
	config      Config
	workerID    string
	logger      *slog.Logger
}

func NewWorker(
	store Store,
	client loreclient.RepositoryDeletionClient,
	credentials loreclient.CredentialProvider,
	config Config,
	logger *slog.Logger,
) (*Worker, error) {
	if store == nil || client == nil || credentials == nil {
		return nil, errors.New("repository deletion dependencies are required")
	}
	if config.PollPeriod <= 0 || config.PollPeriod > time.Minute {
		return nil, errors.New("repository deletion poll period must be no longer than one minute")
	}
	if config.OperationTimeout <= 0 || config.OperationTimeout > 10*time.Minute {
		return nil, errors.New("repository deletion timeout must be no longer than ten minutes")
	}
	if config.LeaseDuration < config.OperationTimeout+leaseSafetyMargin ||
		config.LeaseDuration > 15*time.Minute {
		return nil, errors.New("repository deletion lease must cover the operation timeout and completion")
	}
	if err := loreclient.ValidateServiceSubject(config.ServiceSubject); err != nil {
		return nil, errors.New("repository deletion service subject is invalid")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		store: store, client: client, credentials: credentials, config: config,
		workerID: uuid.NewString(), logger: logger,
	}, nil
}

func (worker *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(worker.config.PollPeriod)
	defer ticker.Stop()
	for {
		worker.runCycle(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (worker *Worker) runCycle(ctx context.Context) {
	for index := 0; index < maxDeletionsPerCycle; index++ {
		claim, err := worker.store.ClaimRepositoryDeletion(ctx, worker.workerID, worker.config.LeaseDuration)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				worker.logger.Error("Repository deletion claim failed", "error", err)
			}
			return
		}
		if claim == nil {
			return
		}
		worker.delete(ctx, *claim)
	}
}

func (worker *Worker) delete(ctx context.Context, claim platform.RepositoryDeletionClaim) {
	repository := loreclient.RepositoryRef{
		CacheKey: claim.RepositoryID, URL: claim.LoreURL, LoreRepositoryID: claim.LoreRepositoryID,
	}
	credential, err := worker.credentials.ForRepository(ctx, loreclient.CredentialRequest{
		Principal: loreclient.ServicePrincipal(
			loreclient.ServicePurposeRepositoryLifecycle,
			worker.config.ServiceSubject,
		),
		Repository: repository,
		Partition:  claim.LoreRepositoryID,
		Scope:      loreclient.ScopeObliterate,
	})
	if err == nil {
		operationContext, cancel := context.WithTimeout(ctx, worker.config.OperationTimeout)
		err = worker.client.DeleteRepositoryWithCredential(operationContext, repository, credential)
		cancel()
	}
	if err != nil {
		worker.logger.Warn(
			"Repository deletion will be retried",
			"repository_id",
			claim.RepositoryID,
			"attempt",
			claim.Attempt,
			"error",
			err,
		)
		if failErr := worker.store.FailRepositoryDeletion(
			ctx,
			worker.workerID,
			claim,
			retryDelay(claim.Attempt),
			err,
		); failErr != nil && !errors.Is(failErr, context.Canceled) {
			worker.logger.Error(
				"Repository deletion failure could not be recorded",
				"repository_id",
				claim.RepositoryID,
				"error",
				failErr,
			)
		}
		return
	}
	if err := worker.store.CompleteRepositoryDeletion(ctx, worker.workerID, claim); err != nil &&
		!errors.Is(err, context.Canceled) {
		worker.logger.Error(
			"Repository deletion completion failed",
			"repository_id",
			claim.RepositoryID,
			"error",
			err,
		)
	}
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := 5 * time.Minute
	for current := 1; current < attempt && delay < 6*time.Hour; current++ {
		delay *= 2
	}
	if delay > 6*time.Hour {
		return 6 * time.Hour
	}
	return delay
}
