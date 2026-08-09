package lore

import (
	"context"
	"errors"
	"strings"
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

func (repository RepositoryRef) CanonicalPartition() string {
	idPartition := strings.TrimSpace(repository.LoreRepositoryID)
	urlPartition := repositoryURLPartition(repository.URL)
	if idPartition != "" {
		return idPartition
	}
	return urlPartition
}

func repositoryURLPartition(repositoryURL string) string {
	parsed, err := parseRepositoryURL(repositoryURL, true)
	if err != nil {
		return ""
	}
	return parsed.Partition
}

type CredentialClient interface {
	RepositoryInfoWithCredential(ctx context.Context, repositoryURL string, credential Credential) (Repository, error)
	BranchesWithCredential(ctx context.Context, repository RepositoryRef, credential Credential) ([]Branch, error)
	CloneWithCredential(
		ctx context.Context,
		repositoryURL string,
		revision string,
		destination string,
		credential Credential,
	) error
}

type ManagedRepositoryClient interface {
	CreateRepositoryWithCredential(
		ctx context.Context,
		repositoryURL string,
		repositoryID string,
		name string,
		description string,
		credential Credential,
	) error
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
	RepositoryInfo(ctx context.Context, repositoryURL string, credential Credential) (Repository, error)
	Branches(ctx context.Context, repository RepositoryRef, credential Credential) ([]Branch, error)
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
	Parents        []string
	Conflicts      []string
}

type MergePushResult struct {
	LocalRevision        string
	RemoteRevision       string
	RemoteSourceRevision string
	RemoteTargetRevision string
	TargetBranchID       string
	Parents              []string
}

var (
	ErrPushAuthorizationRequired = errors.New("Lore merge push authorization is required")
	ErrPushAuthorizationDenied   = errors.New("Lore merge push authorization was denied")
)

type PushAuthorization struct {
	ActorUserID            string
	RepositoryID           string
	RepositoryPartition    string
	OperationID            string
	TargetBranchID         string
	TargetBranchName       string
	ExpectedTargetRevision string
	ProposedRevision       string
	SourceRevision         string
	ParentRevisions        []string
}

type PushAuthorizer interface {
	AuthorizeLoreMergePush(context.Context, PushAuthorization) error
}

type PushAuthorizerFunc func(context.Context, PushAuthorization) error

func (authorizer PushAuthorizerFunc) AuthorizeLoreMergePush(
	ctx context.Context,
	authorization PushAuthorization,
) error {
	if authorizer == nil {
		return ErrPushAuthorizationRequired
	}
	return authorizer(ctx, authorization)
}

type MergeResolution struct {
	Path     string `json:"path"`
	Strategy string `json:"strategy"`
}

type MergeWorkspace struct {
	SourceBranch   string
	TargetBranch   string
	SourceRevision string
	TargetRevision string
	Message        string
	Resolutions    []MergeResolution
}

type CodeClient interface {
	Tree(
		ctx context.Context, repository RepositoryRef, revision, path string, credential Credential, limit int,
	) (Tree, error)
	File(
		ctx context.Context, repository RepositoryRef, revision, path string, credential Credential, maxBytes int64,
	) (File, []byte, error)
	RevisionHistory(
		ctx context.Context, repository RepositoryRef, revision, branch string, credential Credential, limit int,
	) ([]RevisionHistoryEntry, error)
	FileHistory(
		ctx context.Context, repository RepositoryRef, revision, branch, path string, credential Credential, limit int,
	) ([]FileHistoryEntry, error)
	RevisionInfo(ctx context.Context, repository RepositoryRef, revision string, credential Credential) (Revision, error)
	RevisionDiff(
		ctx context.Context, repository RepositoryRef, source, target string, paths []string, credential Credential,
		maxFiles, maxPatchBytes int,
	) (Diff, error)
}

type MergeClient interface {
	StartMerge(ctx context.Context, repository RepositoryRef, operationID, sourceBranch, targetBranch, sourceRevision,
		targetRevision, message string, credential Credential) (MergeStartResult, error)
	EnsureMergeWorkspace(ctx context.Context, repository RepositoryRef, operationID string, workspace MergeWorkspace,
		credential Credential) error
	ResolveMerge(ctx context.Context, repository RepositoryRef, operationID string, workspace MergeWorkspace,
		paths []string, strategy string, credential Credential) (MergeStartResult, error)
	ListConflicts(
		ctx context.Context, repository RepositoryRef, operationID string, workspace MergeWorkspace, paths []string,
		credential Credential,
	) ([]string, error)
	AbortMerge(ctx context.Context, repository RepositoryRef, operationID string, credential Credential) error
	RestartMerge(ctx context.Context, repository RepositoryRef, operationID string, workspace MergeWorkspace,
		paths []string, credential Credential) (MergeStartResult, error)
	PushMerge(
		ctx context.Context, repository RepositoryRef, operationID string, workspace MergeWorkspace,
		stagedRevision string, readCredential Credential, writeCredential Credential, authorizer PushAuthorizer,
	) (MergePushResult, error)
	CleanupMergeWorkspace(ctx context.Context, repository RepositoryRef, operationID string) error
	MergeInto(
		ctx context.Context, repository RepositoryRef, sourceBranch, targetBranch, message string, credential Credential,
	) (MergeStartResult, error)
}
