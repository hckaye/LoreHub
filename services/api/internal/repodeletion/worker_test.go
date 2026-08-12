package repodeletion

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type fakeDeletionStore struct {
	claim      *platform.RepositoryDeletionClaim
	claimCount int
	completed  bool
	failed     error
	retryAfter time.Duration
	workerID   string
}

func (store *fakeDeletionStore) ClaimRepositoryDeletion(
	_ context.Context,
	workerID string,
	_ time.Duration,
) (*platform.RepositoryDeletionClaim, error) {
	store.workerID = workerID
	store.claimCount++
	if store.claimCount > 1 {
		return nil, nil
	}
	return store.claim, nil
}

func (store *fakeDeletionStore) FailRepositoryDeletion(
	_ context.Context,
	workerID string,
	_ platform.RepositoryDeletionClaim,
	retryAfter time.Duration,
	failure error,
) error {
	if workerID != store.workerID {
		return errors.New("worker ID changed")
	}
	store.failed = failure
	store.retryAfter = retryAfter
	return nil
}

func (store *fakeDeletionStore) CompleteRepositoryDeletion(
	_ context.Context,
	workerID string,
	_ platform.RepositoryDeletionClaim,
) error {
	if workerID != store.workerID {
		return errors.New("worker ID changed")
	}
	store.completed = true
	return nil
}

type fakeDeletionClient struct {
	err        error
	repository loreclient.RepositoryRef
	credential loreclient.Credential
}

func (client *fakeDeletionClient) DeleteRepositoryWithCredential(
	_ context.Context,
	repository loreclient.RepositoryRef,
	credential loreclient.Credential,
) error {
	client.repository = repository
	client.credential = credential
	return client.err
}

type fakeCredentialProvider struct {
	request loreclient.CredentialRequest
	err     error
}

func (provider *fakeCredentialProvider) ForRepository(
	_ context.Context,
	request loreclient.CredentialRequest,
) (loreclient.Credential, error) {
	provider.request = request
	if provider.err != nil {
		return loreclient.Credential{}, provider.err
	}
	return loreclient.Credential{
		Partition: request.Partition, Scope: request.Scope, Identity: request.Principal.Subject,
		Principal: request.Principal, InsecureDevelopment: true,
	}, nil
}

func TestWorkerPermanentlyDeletesClaimedRepository(t *testing.T) {
	claim := deletionClaim()
	store := &fakeDeletionStore{claim: &claim}
	client := &fakeDeletionClient{}
	credentials := &fakeCredentialProvider{}
	worker := newTestWorker(t, store, client, credentials)
	worker.runCycle(context.Background())
	if !store.completed || store.failed != nil || store.claimCount != 2 {
		t.Fatalf("unexpected store state: %+v", store)
	}
	if client.repository.URL != claim.LoreURL || client.repository.LoreRepositoryID != claim.LoreRepositoryID {
		t.Fatalf("deletion repository = %+v", client.repository)
	}
	request := credentials.request
	if request.Scope != loreclient.ScopeObliterate ||
		request.Principal.ServicePurpose != loreclient.ServicePurposeRepositoryLifecycle ||
		request.Principal.Subject != "repository-lifecycle-subject" {
		t.Fatalf("deletion credential request = %+v", request)
	}
}

func TestWorkerRecordsDeletionFailureForRetry(t *testing.T) {
	claim := deletionClaim()
	claim.Attempt = 3
	store := &fakeDeletionStore{claim: &claim}
	client := &fakeDeletionClient{err: errors.New("Lore unavailable")}
	worker := newTestWorker(t, store, client, &fakeCredentialProvider{})
	worker.runCycle(context.Background())
	if store.completed || store.failed == nil || store.failed.Error() != "Lore unavailable" {
		t.Fatalf("unexpected failed deletion state: %+v", store)
	}
	if store.retryAfter != 20*time.Minute {
		t.Fatalf("retry delay = %s, want 20m", store.retryAfter)
	}
}

func newTestWorker(
	t *testing.T,
	store Store,
	client loreclient.RepositoryDeletionClient,
	credentials loreclient.CredentialProvider,
) *Worker {
	t.Helper()
	worker, err := NewWorker(store, client, credentials, Config{
		PollPeriod: time.Second, OperationTimeout: time.Second,
		LeaseDuration: 31 * time.Second, ServiceSubject: "repository-lifecycle-subject",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

func deletionClaim() platform.RepositoryDeletionClaim {
	return platform.RepositoryDeletionClaim{
		RepositoryID: "repository-1", OrganizationID: "organization-1",
		Owner: "acme", Slug: "game", LoreRepositoryID: "0123456789abcdef0123456789abcdef",
		LoreURL: "lores://lore.example/0123456789abcdef0123456789abcdef", Attempt: 1,
	}
}
