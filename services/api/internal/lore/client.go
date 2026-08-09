package lore

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("Lore resource not found")

type Repository struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	URL           string    `json:"url"`
	DefaultBranch string    `json:"defaultBranch"`
	Creator       string    `json:"creator"`
	CreatedAt     time.Time `json:"createdAt"`
}

type RepositoryRef struct {
	CacheKey         string
	URL              string
	LoreRepositoryID string
	DefaultBranch    string
}

type Branch struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Category       string    `json:"category"`
	LatestRevision string    `json:"latestRevision"`
	Creator        string    `json:"creator"`
	CreatedAt      time.Time `json:"createdAt"`
	Current        bool      `json:"current"`
	Archived       bool      `json:"archived"`
}

type Client interface {
	RepositoryInfo(ctx context.Context, repositoryURL string, identity string) (Repository, error)
	Branches(ctx context.Context, repository RepositoryRef, identity string) ([]Branch, error)
}

type TreeEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Mode    uint16 `json:"mode"`
	Size    uint64 `json:"size"`
	Address string `json:"address,omitempty"`
}

type Tree struct {
	Revision string      `json:"revision"`
	Path     string      `json:"path"`
	Entries  []TreeEntry `json:"entries"`
	HasMore  bool        `json:"hasMore"`
}

type File struct {
	Path        string `json:"path"`
	Revision    string `json:"revision"`
	Kind        string `json:"kind"`
	Mode        uint16 `json:"mode"`
	Size        uint64 `json:"size"`
	Binary      bool   `json:"binary"`
	BinaryKnown bool   `json:"binaryKnown"`
	Truncated   bool   `json:"truncated"`
	Content     string `json:"content,omitempty"`
}

type Revision struct {
	Revision  string    `json:"revision"`
	Number    uint64    `json:"number"`
	Parents   []string  `json:"parents"`
	Author    string    `json:"author,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
	Message   string    `json:"message,omitempty"`
}

type RevisionHistoryEntry struct {
	Revision string   `json:"revision"`
	Number   uint64   `json:"number"`
	Parents  []string `json:"parents"`
}

type FileHistoryEntry struct {
	Path     string   `json:"path"`
	Revision string   `json:"revision"`
	Number   uint64   `json:"number"`
	Parents  []string `json:"parents"`
	Action   string   `json:"action"`
	Size     uint64   `json:"size"`
}

type DiffFile struct {
	Path        string `json:"path"`
	Action      string `json:"action"`
	Patch       string `json:"patch,omitempty"`
	Binary      bool   `json:"binary"`
	BinaryKnown bool   `json:"binaryKnown"`
	Truncated   bool   `json:"truncated"`
}

type Diff struct {
	Source    string     `json:"source"`
	Target    string     `json:"target"`
	Files     []DiffFile `json:"files"`
	HasMore   bool       `json:"hasMore"`
	Truncated bool       `json:"truncated"`
}

type MergeStartResult struct {
	SourceRevision string
	TargetRevision string
	StagedRevision string
	Conflicts      []string
}

type MergePushResult struct {
	LocalRevision  string
	RemoteRevision string
	AlreadyPushed  bool
}

type CodeClient interface {
	Tree(ctx context.Context, repository RepositoryRef, revision, path, identity string, limit int) (Tree, error)
	File(
		ctx context.Context, repository RepositoryRef, revision, path, identity string, maxBytes int64,
	) (File, []byte, error)
	RevisionHistory(
		ctx context.Context, repository RepositoryRef, revision, branch, identity string, limit int,
	) ([]RevisionHistoryEntry, error)
	FileHistory(
		ctx context.Context, repository RepositoryRef, revision, branch, path, identity string, limit int,
	) ([]FileHistoryEntry, error)
	RevisionInfo(ctx context.Context, repository RepositoryRef, revision, identity string) (Revision, error)
	RevisionDiff(ctx context.Context, repository RepositoryRef, source, target string, paths []string, identity string,
		maxFiles, maxPatchBytes int) (Diff, error)
}

type MergeClient interface {
	StartMerge(ctx context.Context, repository RepositoryRef, operationID, sourceBranch, targetBranch, sourceRevision,
		targetRevision, message, identity string) (MergeStartResult, error)
	ResolveMerge(ctx context.Context, repository RepositoryRef, operationID string, paths []string, strategy,
		identity string) (string, error)
	ListConflicts(
		ctx context.Context, repository RepositoryRef, operationID string, paths []string, identity string,
	) ([]string, error)
	AbortMerge(ctx context.Context, repository RepositoryRef, operationID, identity string) error
	RestartMerge(ctx context.Context, repository RepositoryRef, operationID, sourceBranch, targetBranch,
		sourceRevision, targetRevision string, paths []string, identity string) ([]string, error)
	PushMerge(
		ctx context.Context, repository RepositoryRef, operationID, targetBranch, identity string,
	) (MergePushResult, error)
	CleanupMergeWorkspace(ctx context.Context, repository RepositoryRef, operationID string) error
	MergeInto(
		ctx context.Context, repository RepositoryRef, sourceBranch, targetBranch, message, identity string,
	) (MergeStartResult, error)
}
