package runnerclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/runner"
)

type Artifact struct {
	Name    string
	Content []byte
}

type ExecutionResult struct {
	Conclusion string
	Log        []byte
	Artifacts  []Artifact
}

type Executor interface {
	Execute(context.Context, runner.Job, *Client) (ExecutionResult, error)
}

type ShellExecutorConfig struct {
	Environment      string
	LoreBinary       string
	ActBinary        string
	WorkDirectory    string
	ServerURL        string
	ActionSourceURL  string
	PlatformImage    string
	LogMaxBytes      int64
	LogMaxLineBytes  int
	ArtifactMaxCount int
	ArtifactMaxBytes int64
	ArtifactTotalMax int64
}

type ShellExecutor struct {
	config ShellExecutorConfig
}

func NewShellExecutor(config ShellExecutorConfig) (*ShellExecutor, error) {
	if strings.TrimSpace(config.Environment) == "" {
		config.Environment = "production"
	}
	if strings.TrimSpace(config.LoreBinary) == "" {
		config.LoreBinary = "lore"
	}
	if strings.TrimSpace(config.ActBinary) == "" {
		config.ActBinary = "act"
	}
	for name, binary := range map[string]string{"Lore": config.LoreBinary, "act": config.ActBinary} {
		if _, err := exec.LookPath(binary); err != nil {
			return nil, fmt.Errorf("%s executable %q is unavailable: %w", name, binary, err)
		}
	}
	if strings.TrimSpace(config.WorkDirectory) == "" {
		return nil, errors.New("runner work directory is required")
	}
	if err := os.MkdirAll(config.WorkDirectory, 0o750); err != nil {
		return nil, fmt.Errorf("create runner work directory: %w", err)
	}
	if strings.TrimSpace(config.ActionSourceURL) == "" {
		config.ActionSourceURL = "https://github.com"
	}
	if strings.TrimSpace(config.PlatformImage) == "" {
		config.PlatformImage = runner.DefaultUbuntuLatestImage
	}
	if config.LogMaxBytes <= 0 {
		config.LogMaxBytes = 10 << 20
	}
	if config.LogMaxLineBytes <= 0 {
		config.LogMaxLineBytes = 1 << 20
	}
	if config.ArtifactMaxCount <= 0 {
		config.ArtifactMaxCount = 100
	}
	if config.ArtifactMaxBytes <= 0 {
		config.ArtifactMaxBytes = 100 << 20
	}
	if config.ArtifactTotalMax <= 0 {
		config.ArtifactTotalMax = 500 << 20
	}
	return &ShellExecutor{config: config}, nil
}

func (executor *ShellExecutor) Execute(
	ctx context.Context,
	job runner.Job,
	client *Client,
) (ExecutionResult, error) {
	workspace, err := os.MkdirTemp(executor.config.WorkDirectory, "job-"+job.ID+"-")
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("create runner workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	contextValues, err := client.ExecutionContext(ctx, job.ID)
	if err != nil {
		return ExecutionResult{}, err
	}
	jobToken, err := client.JobToken(ctx, job.ID)
	if err != nil {
		return ExecutionResult{}, err
	}
	contextValues.Secrets["GITHUB_TOKEN"] = jobToken.Token
	var logBuffer boundedBuffer
	logBuffer.maximum = executor.config.LogMaxBytes
	masker := runner.NewMaskingLogWriter(
		&logBuffer, contextValues.Secrets, executor.config.LogMaxLineBytes, nil,
	)
	repositoryPath := filepath.Join(workspace, "repository")
	clone := exec.CommandContext(
		ctx, executor.config.LoreBinary, "clone", "--revision", job.Revision, job.LoreURL, repositoryPath,
	)
	clone.Stdout = masker
	clone.Stderr = masker
	clone.Env = append(safeCommandEnvironment(), "LOREHUB_ACTIONS_JOB_TOKEN="+jobToken.Token)
	if err := clone.Run(); err != nil {
		_ = masker.Flush()
		return executionFailure(logBuffer.Bytes(), fmt.Errorf("clone Lore revision: %w", err)), nil
	}
	if _, err := runner.AdaptWorkflow(repositoryPath, job.WorkflowPath); err != nil {
		_ = masker.Flush()
		return executionFailure(logBuffer.Bytes(), err), nil
	}
	eventPath := filepath.Join(workspace, "event.json")
	if err := os.WriteFile(eventPath, job.EventPayload, 0o600); err != nil {
		return ExecutionResult{}, fmt.Errorf("write runner event: %w", err)
	}
	artifactPath := filepath.Join(workspace, "artifacts")
	if err := os.MkdirAll(artifactPath, 0o750); err != nil {
		return ExecutionResult{}, fmt.Errorf("create runner artifact directory: %w", err)
	}
	secretPath, err := runner.WriteActionSecretFile(workspace, contextValues.Secrets)
	if err != nil {
		return ExecutionResult{}, err
	}
	defer runner.CleanupActionSecretFile(secretPath)
	workflowPath := filepath.Join(repositoryPath, filepath.FromSlash(job.WorkflowPath))
	actionRepositories, err := runner.PrepareRemoteActions(
		ctx, workflowPath, filepath.Join(workspace, "remote-actions"), executor.config.ActionSourceURL,
		executor.config.Environment,
	)
	if err != nil {
		return ExecutionResult{}, err
	}
	serverURL := strings.TrimRight(executor.config.ServerURL, "/")
	platformImages := make(map[string]string, len(job.RunnerLabels))
	for _, label := range job.RunnerLabels {
		platformImages[label] = executor.config.PlatformImage
	}
	if len(platformImages) == 0 {
		platformImages["self-hosted"] = executor.config.PlatformImage
	}
	arguments := runner.ExternalActArguments(
		job, repositoryPath, workflowPath, eventPath, artifactPath,
		runner.ExternalActInvocation{
			ActionRepositories: actionRepositories,
			PlatformImages:     platformImages,
			GitHub: runner.GitHubContext{
				ServerURL: serverURL, APIURL: serverURL + "/api/v3", GraphQLURL: serverURL + "/api/graphql",
			},
			Variables: contextValues.Variables, SecretFile: secretPath,
			ArtifactServerAddress: "127.0.0.1", NetworkName: "bridge",
			ProxyURL: firstEnvironment("HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"),
		},
	)
	command := exec.CommandContext(ctx, executor.config.ActBinary, arguments...)
	command.Stdout = masker
	command.Stderr = masker
	command.Env = append(safeCommandEnvironment(), "LOREHUB_LORE_REVISION="+job.Revision)
	runErr := runner.RunAct(ctx, command)
	if err := masker.Flush(); err != nil && runErr == nil {
		runErr = err
	}
	artifacts, artifactErr := executor.readArtifacts(artifactPath)
	if artifactErr != nil {
		return ExecutionResult{}, artifactErr
	}
	result := ExecutionResult{Conclusion: "success", Log: logBuffer.Bytes(), Artifacts: artifacts}
	if runErr != nil {
		result.Conclusion = "failure"
		if errors.Is(runErr, context.Canceled) {
			result.Conclusion = "cancelled"
		} else if errors.Is(runErr, context.DeadlineExceeded) {
			result.Conclusion = "timed_out"
		}
	}
	return result, nil
}

func (executor *ShellExecutor) readArtifacts(root string) ([]Artifact, error) {
	artifacts := make([]Artifact, 0)
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("runner artifact is a symbolic link")
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("runner artifact is not a regular file")
		}
		if len(artifacts) >= executor.config.ArtifactMaxCount || info.Size() > executor.config.ArtifactMaxBytes ||
			info.Size() > executor.config.ArtifactTotalMax-total {
			return errors.New("runner artifact quota exceeded")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		artifacts = append(artifacts, Artifact{Name: filepath.ToSlash(name), Content: content})
		total += info.Size()
		return nil
	})
	return artifacts, err
}

func executionFailure(log []byte, err error) ExecutionResult {
	return ExecutionResult{Conclusion: "failure", Log: append(log, []byte("\n"+err.Error()+"\n")...)}
}

type boundedBuffer struct {
	buffer  bytes.Buffer
	maximum int64
}

func (buffer *boundedBuffer) Write(content []byte) (int, error) {
	remaining := buffer.maximum - int64(buffer.buffer.Len())
	if remaining <= 0 {
		return len(content), nil
	}
	if int64(len(content)) > remaining {
		_, _ = buffer.buffer.Write(content[:remaining])
		return len(content), nil
	}
	_, _ = buffer.buffer.Write(content)
	return len(content), nil
}

func (buffer *boundedBuffer) Bytes() []byte {
	return append([]byte(nil), buffer.buffer.Bytes()...)
}

func firstEnvironment(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func safeCommandEnvironment() []string {
	allowed := []string{
		"PATH", "HOME", "DOCKER_HOST", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH", "DOCKER_CONFIG",
		"XDG_RUNTIME_DIR", "TMPDIR", "HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy",
		"NO_PROXY", "no_proxy",
	}
	environment := make([]string, 0, len(allowed))
	for _, name := range allowed {
		if value := os.Getenv(name); value != "" {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}
