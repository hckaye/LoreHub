package runner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

type BranchClient interface {
	Branches(context.Context, loreclient.RepositoryRef, loreclient.Credential) ([]loreclient.Branch, error)
}

type workflowRevisionClient interface {
	CloneWithCredential(context.Context, string, string, string, loreclient.Credential) error
}

type Poller struct {
	store           *Store
	lore            BranchClient
	credentials     loreclient.CredentialProvider
	observerSubject string
	period          time.Duration
	logger          *slog.Logger
	platforms       map[string]string
}

func NewPoller(
	store *Store,
	lore BranchClient,
	credentials loreclient.CredentialProvider,
	observerSubject string,
	period time.Duration,
	logger *slog.Logger,
	platformImages ...map[string]string,
) *Poller {
	platforms := DefaultRunnerPlatformImages()
	if len(platformImages) > 0 && platformImages[0] != nil {
		for label, image := range platformImages[0] {
			platforms[label] = image
		}
	}
	return &Poller{
		store: store, lore: lore, credentials: credentials, observerSubject: observerSubject,
		period: period, logger: logger, platforms: platforms,
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
	if poller.credentials == nil {
		return fmt.Errorf("branch observer requires a scoped Lore credential provider")
	}
	if err := loreclient.ValidateServiceSubject(poller.observerSubject); err != nil {
		return fmt.Errorf("branch observer requires a dedicated service principal: %w", err)
	}
	repositories, err := poller.store.Repositories(ctx)
	if err != nil {
		return err
	}
	for _, repository := range repositories {
		partition, err := poller.store.RepositoryPartition(ctx, repository.ID)
		if err != nil {
			poller.logger.Error(
				"could not resolve active repository Lore partition",
				"repository", repository.Owner+"/"+repository.Slug,
				"error", err,
			)
			continue
		}
		repository.LoreRepositoryID = partition
		ref := loreclient.RepositoryRef{
			CacheKey: repository.ID, URL: repository.LoreURL, LoreRepositoryID: repository.LoreRepositoryID,
		}
		credential, err := poller.credentials.ForRepository(ctx, loreclient.CredentialRequest{
			Principal:  loreclient.ServicePrincipal(loreclient.ServicePurposeObserver, poller.observerSubject),
			Repository: ref, Partition: repository.LoreRepositoryID, Scope: loreclient.ScopeRead,
		})
		if err != nil {
			poller.logger.Error("could not mint scoped Lore polling credential",
				"repository", repository.Owner+"/"+repository.Slug, "error", err)
			continue
		}
		branches, err := poller.lore.Branches(ctx, ref, credential)
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
			if isZeroLoreRevision(branch.LatestRevision) {
				if _, err := poller.store.ObserveBranch(ctx, repository, ObservedBranch{
					ID: branch.ID, Name: branch.Name, LatestRevision: branch.LatestRevision,
				}); err != nil {
					return fmt.Errorf(
						"observe empty %s/%s branch %s: %w",
						repository.Owner,
						repository.Slug,
						branch.Name,
						err,
					)
				}
				continue
			}
			var workflows []WorkflowDefinition
			workspace, err := os.MkdirTemp("", "lorehub-workflow-")
			if err != nil {
				return fmt.Errorf("create workflow inspection workspace: %w", err)
			}
			var cloneErr error
			switch client := poller.lore.(type) {
			case workflowRevisionClient:
				cloneErr = client.CloneWithCredential(
					ctx, repository.LoreURL, branch.LatestRevision, workspace, credential,
				)
			case loreclient.CredentialRevisionClient:
				cloneErr = client.CloneRevisionWithCredential(
					ctx, ref, credential, branch.LatestRevision, workspace,
				)
			default:
				cloneErr = fmt.Errorf("Lore client cannot inspect workflows at exact revisions")
			}
			if cloneErr == nil {
				workflows, cloneErr = DiscoverWorkflows(workspace, poller.platforms)
			}
			removeErr := os.RemoveAll(workspace)
			if cloneErr != nil {
				poller.logger.Error(
					"could not inspect workflows at exact Lore revision",
					"repository", repository.Owner+"/"+repository.Slug,
					"revision", branch.LatestRevision,
					"error", cloneErr,
				)
				continue
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
			if branch.Name == repository.DefaultBranch {
				if _, err := poller.store.EnqueueScheduledRuns(
					ctx, repository, branch.LatestRevision, time.Now().UTC(),
				); err != nil {
					return fmt.Errorf("enqueue scheduled runs for %s/%s: %w", repository.Owner, repository.Slug, err)
				}
			}
		}
		if err := poller.store.ReconcileBranchStates(ctx, repository.ID, observedBranchIDs); err != nil {
			return fmt.Errorf("reconcile %s/%s branch state: %w", repository.Owner, repository.Slug, err)
		}
	}
	return nil
}

func isZeroLoreRevision(revision string) bool {
	return len(revision) == 64 && strings.Trim(revision, "0") == ""
}
