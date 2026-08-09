package lore

import (
	"context"
	"fmt"
	"strings"

	loresdk "github.com/EpicGames/lore-go"
	"github.com/EpicGames/lore-go/types"
)

func workspaceBranchState(ctx context.Context, path, identity string) (string, string, string, error) {
	globals, cleanupGlobals := readGlobals(path, identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRepositoryStatusArgs(types.LoreRepositoryStatusArgs{Staged: true})
	defer cleanupArgs()
	var branchID, branchName, revision string
	op := loresdk.RepositoryStatus(&globals, &args)
	op.Callback(func(event *types.LoreEventFFI, _ uint64) {
		if event.Tag != types.LoreEventTag_REPOSITORY_STATUS_REVISION {
			return
		}
		data, ok := event.GetData().(*types.LoreRepositoryStatusRevisionEventDataFFI)
		if !ok {
			return
		}
		branchID = data.Branch.String()
		branchName = data.BranchName.String()
		revision = data.Revision.String()
	})
	if err := waitLore(ctx, op.Wait); err != nil {
		return "", "", "", fmt.Errorf("read Lore merge branch state: %w", err)
	}
	return branchID, branchName, revision, nil
}

func isZeroRevision(value string) bool {
	return value == "" || value == strings.Repeat("0", 64)
}
