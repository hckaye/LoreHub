package runner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

type BranchClient interface {
	Branches(ctx context.Context, repository loreclient.RepositoryRef, identity string) ([]loreclient.Branch, error)
}

type Poller struct {
	store       *Store
	lore        BranchClient
	credentials CredentialProvider
	period      time.Duration
	logger      *slog.Logger
}

func NewPoller(
	store *Store,
	lore BranchClient,
	credentials CredentialProvider,
	period time.Duration,
	logger *slog.Logger,
) *Poller {
	if credentials == nil {
		credentials = missingCredentialProvider{}
	}
	return &Poller{store: store, lore: lore, credentials: credentials, period: period, logger: logger}
}

type missingCredentialProvider struct{}

func (missingCredentialProvider) Read(context.Context, CredentialSubject, string) (string, error) {
	return "", fmt.Errorf("repository-scoped Lore credential provider is not configured")
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
		identity, err := poller.credentials.Read(ctx, CredentialSubject{
			RepositoryID: repository.ID,
			LoreURL:      repository.LoreURL,
		}, ReadLoreScope)
		if err != nil {
			poller.logger.Error(
				"could not read repository Lore credential",
				"repository", repository.Owner+"/"+repository.Slug,
				"error", err,
			)
			continue
		}
		branches, err := poller.lore.Branches(ctx, loreclient.RepositoryRef{
			CacheKey: repository.ID,
			URL:      repository.LoreURL,
		}, identity)
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
			var workflows []WorkflowDefinition
			revisionClient, canInspectRevision := poller.lore.(loreclient.RevisionClient)
			if !canInspectRevision {
				return fmt.Errorf("Lore client cannot inspect workflows at exact revisions")
			}
			workspace, err := os.MkdirTemp("", "lorehub-workflow-")
			if err != nil {
				return fmt.Errorf("create workflow inspection workspace: %w", err)
			}
			cloneErr := revisionClient.CloneRevision(ctx, loreclient.RepositoryRef{
				CacheKey: repository.ID,
				URL:      repository.LoreURL,
			}, identity, branch.LatestRevision, workspace)
			if cloneErr == nil {
				workflows, cloneErr = DiscoverWorkflows(workspace)
			}
			removeErr := os.RemoveAll(workspace)
			if cloneErr != nil {
				return fmt.Errorf("inspect workflows at Lore revision %s: %w", branch.LatestRevision, cloneErr)
			}
			if removeErr != nil {
				return fmt.Errorf("remove workflow inspection workspace: %w", removeErr)
			}
			queued, err := poller.store.ObserveBranch(ctx, repository, ObservedBranch{
				ID:             branch.ID,
				Name:           branch.Name,
				LatestRevision: branch.LatestRevision,
			}, workflows...)
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
