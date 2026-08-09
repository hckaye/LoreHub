package lore

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	loresdk "github.com/EpicGames/lore-go"
	"github.com/EpicGames/lore-go/types"
)

func TestSDKClientAgainstLoreServer(t *testing.T) {
	repositoryURL := os.Getenv("LOREHUB_TEST_LORE_URL")
	token := os.Getenv("LOREHUB_TEST_LORE_TOKEN")
	authURL := os.Getenv("LOREHUB_TEST_LORE_AUTH_URL")
	identity := os.Getenv("LOREHUB_TEST_LORE_IDENTITY")
	if repositoryURL == "" || token == "" || authURL == "" || identity == "" {
		t.Skip("LOREHUB_TEST_LORE_URL, LOREHUB_TEST_LORE_TOKEN, " +
			"LOREHUB_TEST_LORE_AUTH_URL, and LOREHUB_TEST_LORE_IDENTITY are required")
	}
	client, err := NewSDKClient(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	credential := Credential{Token: token, AuthURL: authURL, Identity: identity}
	repository, err := client.RepositoryInfoWithCredential(ctx, repositoryURL, credential)
	if err != nil {
		t.Fatalf("RepositoryInfo returned an error: %v", err)
	}
	if repository.ID == "" || repository.DefaultBranch == "" {
		t.Fatalf("repository data is incomplete: %#v", repository)
	}
	branches, err := client.BranchesWithCredential(
		ctx,
		RepositoryRef{CacheKey: "integration", URL: repositoryURL},
		credential,
	)
	if err != nil {
		t.Fatalf("Branches returned an error: %v", err)
	}
	for _, branch := range branches {
		if branch.Name == repository.DefaultBranch && branch.LatestRevision != "" {
			return
		}
	}
	t.Fatalf("default branch %q was not returned: %#v", repository.DefaultBranch, branches)
}

func TestSDKClientLoreAuthBoundaryAgainstLoreServer(t *testing.T) {
	primaryURL := os.Getenv("LOREHUB_TEST_LORE_URL")
	otherURL := os.Getenv("LOREHUB_TEST_LORE_OTHER_URL")
	authURL := os.Getenv("LOREHUB_TEST_LORE_AUTH_URL")
	identity := os.Getenv("LOREHUB_TEST_LORE_IDENTITY")
	primaryToken := os.Getenv("LOREHUB_TEST_LORE_READ_TOKEN")
	otherToken := os.Getenv("LOREHUB_TEST_LORE_OTHER_TOKEN")
	baseToken := os.Getenv("LOREHUB_TEST_LORE_BASE_TOKEN")
	negativeTokens := map[string]string{
		"expired":        os.Getenv("LOREHUB_TEST_LORE_EXPIRED_TOKEN"),
		"wrong_issuer":   os.Getenv("LOREHUB_TEST_LORE_WRONG_ISSUER_TOKEN"),
		"wrong_audience": os.Getenv("LOREHUB_TEST_LORE_WRONG_AUDIENCE_TOKEN"),
		"wrong_kid":      os.Getenv("LOREHUB_TEST_LORE_WRONG_KID_TOKEN"),
	}
	if primaryURL == "" || otherURL == "" || authURL == "" || identity == "" || primaryToken == "" ||
		otherToken == "" || baseToken == "" {
		t.Skip("Lore boundary integration credentials and two repository URLs are required")
	}
	for name, token := range negativeTokens {
		if token == "" {
			t.Skip("Lore boundary integration malformed tokens are required")
		}
		if name == "" {
			t.Fatal("negative token name is empty")
		}
	}

	credential := func(token string) Credential {
		return Credential{Token: token, AuthURL: authURL, Identity: identity}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Run("valid_read_gRPC", func(t *testing.T) {
		client, err := NewSDKClient(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		repository, err := client.RepositoryInfoWithCredential(ctx, primaryURL, credential(primaryToken))
		if err != nil {
			t.Fatalf("valid gRPC read was rejected: %v", err)
		}
		if repository.ID == "" {
			t.Fatal("valid gRPC read returned no repository ID")
		}
	})

	t.Run("missing_token_blocks_gRPC", func(t *testing.T) {
		if err := unauthenticatedRepositoryInfo(ctx, primaryURL, "missing-grpc-token-identity"); err == nil {
			t.Fatal("gRPC read without a stored token was accepted")
		}
	})

	t.Run("valid_read_QUIC_and_missing_token", func(t *testing.T) {
		client, err := NewSDKClient(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		repository := RepositoryRef{CacheKey: "quic-boundary", URL: primaryURL}
		if _, err := client.BranchesWithCredential(ctx, repository, credential(primaryToken)); err != nil {
			t.Fatalf("valid QUIC read was rejected: %v", err)
		}
		if _, err := client.Branches(ctx, repository, "missing-token-identity"); err == nil {
			t.Fatal("QUIC read without a stored token was accepted")
		}
	})

	t.Run("base_token_blocks_gRPC_and_QUIC", func(t *testing.T) {
		assertCredentialRejectedOnBothTransports(t, ctx, primaryURL, credential(baseToken))
	})

	t.Run("cross_partition_token_blocks_gRPC_and_QUIC", func(t *testing.T) {
		assertCredentialRejectedOnBothTransports(t, ctx, primaryURL, credential(otherToken))
		assertCredentialRejectedOnBothTransports(t, ctx, otherURL, credential(primaryToken))
	})

	for name, token := range negativeTokens {
		name, token := name, token
		t.Run(name+"_blocks_gRPC_and_QUIC", func(t *testing.T) {
			assertCredentialRejectedOnBothTransports(t, ctx, primaryURL, credential(token))
		})
	}

	t.Run("read_token_blocks_write", func(t *testing.T) {
		client, err := NewSDKClient(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		err = client.CreateRepositoryWithCredential(ctx, primaryURL,
			"0123456789abcdef0123456789abcdef", "read-only-boundary", "", credential(primaryToken))
		if err == nil {
			t.Fatal("Lore accepted repository creation with a read-only token")
		}
	})
}

func assertCredentialRejectedOnBothTransports(
	t *testing.T,
	ctx context.Context,
	repositoryURL string,
	credential Credential,
) {
	t.Helper()
	grpcClient, err := NewSDKClient(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := grpcClient.RepositoryInfoWithCredential(ctx, repositoryURL, credential); err == nil {
		t.Fatal("gRPC repository read accepted an unauthorized Lore credential")
	}
	quicClient, err := NewSDKClient(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := quicClient.BranchesWithCredential(ctx, RepositoryRef{
		CacheKey: "negative-quic-boundary", URL: repositoryURL,
	}, credential); err == nil {
		t.Fatal("QUIC branch read accepted an unauthorized Lore credential")
	}
}

func unauthenticatedRepositoryInfo(ctx context.Context, repositoryURL, identity string) error {
	globals, cleanupGlobals := types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		Identity: identity,
		Remote:   true,
		InMemory: true,
	})
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRepositoryInfoArgs(types.LoreRepositoryInfoArgs{
		RepositoryUrl: repositoryURL,
	})
	defer cleanupArgs()
	events, err := loresdk.RepositoryInfo(&globals, &args).
		FilterByType(types.LoreEventTag_REPOSITORY_DATA).
		Collect()
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return errors.New("unauthenticated Lore read returned no repository data")
	}
	return nil
}
