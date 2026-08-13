package lore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	loresdk "github.com/EpicGames/lore-go"
	"github.com/EpicGames/lore-go/types"
)

func (client *SDKClient) MirrorRepository(ctx context.Context, input RepositoryMirrorInput) error {
	if err := client.validateRepository(input.Source); err != nil {
		return fmt.Errorf("validate migration source: %w", err)
	}
	if err := client.validateRepository(input.Target); err != nil {
		return fmt.Errorf("validate migration target: %w", err)
	}
	if input.Source.LoreRepositoryID != input.Target.LoreRepositoryID {
		return errors.New("migration source and target partitions must match")
	}
	if err := ValidateCredential(input.Source, input.SourceCredential, ScopeRead); err != nil {
		return fmt.Errorf("validate migration source credential: %w", err)
	}
	if err := ValidateCredential(input.Target, input.TargetCredential, ScopeAdmin); err != nil {
		return fmt.Errorf("validate migration target credential: %w", err)
	}

	if err := client.CreateRepositoryWithCredential(ctx, input.Target.URL, input.Target.LoreRepositoryID,
		input.Name, input.Description, input.TargetCredential); err != nil {
		return fmt.Errorf("create target Lore repository: %w", err)
	}
	branches, err := client.Branches(ctx, input.Source, input.SourceCredential)
	if err != nil {
		return fmt.Errorf("list source Lore branches: %w", err)
	}
	activeBranches := make([]Branch, 0, len(branches))
	for _, branch := range branches {
		if branch.Archived || branch.Name == "" || branch.LatestRevision == "" {
			continue
		}
		activeBranches = append(activeBranches, branch)
	}
	if len(activeBranches) == 0 {
		return errors.New("source Lore repository has no active branches to migrate")
	}

	targetTransportURL, err := client.transportRepositoryURL(ctx, input.Target.URL)
	if err != nil {
		return fmt.Errorf("resolve target Lore transport: %w", err)
	}
	workspace, err := os.MkdirTemp(client.cacheDirectory, "migration-"+input.Source.LoreRepositoryID+"-")
	if err != nil {
		return fmt.Errorf("create Lore migration workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	if err := client.CloneWithCredential(ctx, input.Source.URL, "", workspace, input.SourceCredential); err != nil {
		return fmt.Errorf("clone source Lore repository: %w", err)
	}
	if err := replaceLoreRemote(workspace, targetTransportURL); err != nil {
		return fmt.Errorf("point migration workspace at target Lore server: %w", err)
	}
	if err := client.authenticate(ctx, workspace, targetTransportURL, input.TargetCredential); err != nil {
		return fmt.Errorf("authenticate target Lore repository: %w", err)
	}
	for _, branch := range activeBranches {
		if err := client.pushMigratedBranch(ctx, workspace, branch, input.TargetCredential.Identity); err != nil {
			return fmt.Errorf("mirror Lore branch %q: %w", branch.Name, err)
		}
	}
	targetBranches, err := client.Branches(ctx, input.Target, input.TargetCredential)
	if err != nil {
		return fmt.Errorf("verify target Lore branches: %w", err)
	}
	for _, sourceBranch := range activeBranches {
		targetBranch, found := branchByName(targetBranches, sourceBranch.Name)
		if !found || targetBranch.LatestRevision != sourceBranch.LatestRevision {
			return fmt.Errorf("target Lore branch %q does not match source revision", sourceBranch.Name)
		}
	}
	return nil
}

func (client *SDKClient) pushMigratedBranch(
	ctx context.Context,
	workspace string,
	branch Branch,
	identity string,
) error {
	globals, cleanupGlobals := readGlobals(workspace, identity)
	defer cleanupGlobals()
	// BranchSwitch resolves the source branch's exact tip in the cloned store.
	switchArgs, cleanupSwitchArgs := types.NewLoreBranchSwitchArgs(types.LoreBranchSwitchArgs{
		Branch: branch.Name, Revision: branch.LatestRevision, Reset: true,
	})
	if err := waitLore(ctx, loresdk.BranchSwitch(&globals, &switchArgs).Wait); err != nil {
		cleanupSwitchArgs()
		return fmt.Errorf("switch workspace to revision: %w", err)
	}
	cleanupSwitchArgs()
	pushArgs, cleanupPushArgs := types.NewLoreBranchPushArgs(types.LoreBranchPushArgs{
		Branch: branch.Name,
	})
	defer cleanupPushArgs()
	if err := waitLore(ctx, loresdk.BranchPush(&globals, &pushArgs).Wait); err != nil {
		return fmt.Errorf("push target branch: %w", err)
	}
	return nil
}

func replaceLoreRemote(workspace string, targetURL string) error {
	if strings.TrimSpace(targetURL) == "" {
		return errors.New("target Lore URL is required")
	}
	configPath := filepath.Join(workspace, ".lore", "config.toml")
	contents, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read Lore workspace config: %w", err)
	}
	lines := strings.SplitAfter(string(contents), "\n")
	found := false
	for index, line := range lines {
		lineEnding := ""
		if strings.HasSuffix(line, "\n") {
			lineEnding = "\n"
			line = strings.TrimSuffix(line, "\n")
		}
		key, _, hasValue := strings.Cut(strings.TrimSpace(line), "=")
		if !hasValue || strings.TrimSpace(key) != "remote_url" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		lines[index] = "remote_url = " + strconv.Quote(targetURL) + lineEnding
		found = true
	}
	if !found {
		return errors.New("Lore workspace config has no remote_url")
	}
	fileInfo, err := os.Stat(configPath)
	if err != nil {
		return fmt.Errorf("stat Lore workspace config: %w", err)
	}
	if err := os.WriteFile(configPath, []byte(strings.Join(lines, "")), fileInfo.Mode().Perm()); err != nil {
		return fmt.Errorf("write Lore workspace config: %w", err)
	}
	return nil
}
