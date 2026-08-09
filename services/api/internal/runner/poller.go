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
	store     *Store
	lore      BranchClient
	issuer    CredentialIssuer
	principal CredentialPrincipal
	period    time.Duration
	logger    *slog.Logger
	platforms map[string]string
}

func NewPoller(
	store *Store,
	lore BranchClient,
	issuer CredentialIssuer,
	principal CredentialPrincipal,
	period time.Duration,
	logger *slog.Logger,
	platformImages ...map[string]string,
) *Poller {
	if issuer == nil {
		issuer = NewFailClosedCredentialIssuer()
	}
	platforms := DefaultRunnerPlatformImages()
	if len(platformImages) > 0 && platformImages[0] != nil {
		for label, image := range platformImages[0] {
			platforms[label] = image
		}
	}
	return &Poller{
		store: store, lore: lore, issuer: issuer, principal: principal, period: period, logger: logger,
		platforms: platforms,
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
		credential, err := issueLoreCredential(ctx, poller.issuer, poller.principal, repository.ID, repository.LoreURL)
		if err != nil {
			poller.logger.Error(
				"could not read repository Lore credential",
				"repository", repository.Owner+"/"+repository.Slug,
				"error", err,
			)
			continue
		}
		repositoryRef := loreclient.RepositoryRef{CacheKey: repository.ID, URL: repository.LoreURL}
		var branches []loreclient.Branch
		if client, ok := poller.lore.(loreclient.CredentialBranchClient); ok {
			branches, err = client.BranchesWithCredential(ctx, repositoryRef,
				loreCredential(credential, poller.principal, repository.ID,
					issuerIsDevelopmentOnly(poller.issuer)))
		} else if credential.Identity != "" && issuerIsDevelopmentOnly(poller.issuer) {
			branches, err = poller.lore.Branches(ctx, repositoryRef, credential.Identity)
		} else {
			err = fmt.Errorf("the configured Lore client does not accept token or AuthURL credentials")
		}
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
			var cloneErr error
			if client, ok := poller.lore.(loreclient.CredentialRevisionClient); ok {
				cloneErr = client.CloneRevisionWithCredential(
					ctx, repositoryRef, loreCredential(credential, poller.principal, repository.ID,
						issuerIsDevelopmentOnly(poller.issuer)), branch.LatestRevision, workspace,
				)
			} else if credential.Identity != "" && issuerIsDevelopmentOnly(poller.issuer) {
				cloneErr = revisionClient.CloneRevision(
					ctx, repositoryRef, credential.Identity, branch.LatestRevision, workspace,
				)
			} else {
				cloneErr = fmt.Errorf("the configured Lore client does not accept token or AuthURL credentials")
			}
			if cloneErr == nil {
				workflows, cloneErr = DiscoverWorkflows(workspace, poller.platforms)
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
			if branch.Name == repository.DefaultBranch {
				if _, err := poller.store.EnqueueScheduledRuns(
					ctx, repository, branch.LatestRevision, time.Now().UTC(),
				); err != nil {
					return fmt.Errorf("enqueue scheduled runs for %s/%s: %w", repository.Owner, repository.Slug, err)
				}
			}
		}
	}
	return nil
}
