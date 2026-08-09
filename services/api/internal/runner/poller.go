package runner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

type BranchClient interface {
	Branches(ctx context.Context, repository loreclient.RepositoryRef, identity string) ([]loreclient.Branch, error)
}

type Poller struct {
	store    *Store
	lore     BranchClient
	identity string
	period   time.Duration
	logger   *slog.Logger
}

func NewPoller(
	store *Store,
	lore BranchClient,
	identity string,
	period time.Duration,
	logger *slog.Logger,
) *Poller {
	return &Poller{store: store, lore: lore, identity: identity, period: period, logger: logger}
}

func (poller *Poller) Run(ctx context.Context) error {
	if err := poller.poll(ctx); err != nil {
		poller.logger.Error("initial Lore branch poll failed", "error", err)
	}
	ticker := time.NewTicker(poller.period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := poller.poll(ctx); err != nil {
				poller.logger.Error("Lore branch poll failed", "error", err)
			}
		}
	}
}

func (poller *Poller) poll(ctx context.Context) error {
	repositories, err := poller.store.Repositories(ctx)
	if err != nil {
		return err
	}
	for _, repository := range repositories {
		branches, err := poller.lore.Branches(ctx, loreclient.RepositoryRef{
			CacheKey: repository.ID,
			URL:      repository.LoreURL,
		}, poller.identity)
		if err != nil {
			poller.logger.Error(
				"could not read Lore branches",
				"repository", repository.Owner+"/"+repository.Slug,
				"error", err,
			)
			continue
		}
		for _, branch := range branches {
			if branch.Archived || branch.LatestRevision == "" {
				continue
			}
			queued, err := poller.store.ObserveBranch(ctx, repository, ObservedBranch{
				ID:             branch.ID,
				Name:           branch.Name,
				LatestRevision: branch.LatestRevision,
			})
			if err != nil {
				return fmt.Errorf("observe %s/%s branch %s: %w", repository.Owner, repository.Slug, branch.Name, err)
			}
			if queued {
				poller.logger.Info(
					"queued CI run for Lore branch update",
					"repository", repository.Owner+"/"+repository.Slug,
					"branch", branch.Name,
					"revision", branch.LatestRevision,
				)
			}
		}
	}
	return nil
}
