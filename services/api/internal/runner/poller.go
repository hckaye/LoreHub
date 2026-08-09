package runner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

type BranchClient interface {
	BranchesWithCredential(
		ctx context.Context,
		repository loreclient.RepositoryRef,
		credential loreclient.Credential,
	) ([]loreclient.Branch, error)
}

type CredentialIssuer interface {
	IssueServiceResourceToken(
		ctx context.Context,
		principalName string,
		resourceID string,
		requested []string,
	) (string, error)
	AuthURL() string
}

type Poller struct {
	store                 *Store
	lore                  BranchClient
	issuer                CredentialIssuer
	observerPrincipalName string
	period                time.Duration
	logger                *slog.Logger
}

func NewPoller(
	store *Store,
	lore BranchClient,
	issuer CredentialIssuer,
	observerPrincipalName string,
	period time.Duration,
	logger *slog.Logger,
) *Poller {
	return &Poller{
		store: store, lore: lore, issuer: issuer, observerPrincipalName: observerPrincipalName,
		period: period, logger: logger,
	}
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
		if poller.issuer == nil || poller.observerPrincipalName == "" {
			return fmt.Errorf("branch observer requires a dedicated service principal")
		}
		token, err := poller.issuer.IssueServiceResourceToken(ctx, poller.observerPrincipalName,
			"urc-"+repository.LoreRepositoryID, []string{"read"})
		if err != nil {
			poller.logger.Error("could not mint scoped Lore polling credential",
				"repository", repository.Owner+"/"+repository.Slug, "error", err)
			continue
		}
		branches, err := poller.lore.BranchesWithCredential(ctx, loreclient.RepositoryRef{
			CacheKey: repository.ID,
			URL:      repository.LoreURL,
		}, loreclient.Credential{
			Token: token, AuthURL: poller.issuer.AuthURL(), Identity: poller.observerPrincipalName,
		})
		if err != nil {
			poller.logger.Error(
				"could not read Lore branches",
				"repository", repository.Owner+"/"+repository.Slug,
				"error", err,
			)
			continue
		}
		observedBranchIDs := make([]string, 0, len(branches))
		for _, branch := range branches {
			if branch.Archived || branch.LatestRevision == "" {
				continue
			}
			observedBranchIDs = append(observedBranchIDs, branch.ID)
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
		if err := poller.store.ReconcileBranchStates(ctx, repository.ID, observedBranchIDs); err != nil {
			return fmt.Errorf("reconcile %s/%s branch state: %w", repository.Owner, repository.Slug, err)
		}
	}
	return nil
}
