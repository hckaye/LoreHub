package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

type WorkerConfig struct {
	Environment           string
	LoreBinary            string
	CredentialIssuer      CredentialIssuer
	CredentialPrincipal   CredentialPrincipal
	Credentials           loreclient.CredentialProvider
	ServicePrincipal      string
	JobTokenIssuer        JobTokenIssuer
	JobTokenRESTScope     string
	JobTokenGraphQLScope  string
	ExecutionResolver     ExecutionContextResolver
	GitHubContext         GitHubContext
	ActionSourceURL       string
	PlatformImages        map[string]string
	RevisionClient        loreclient.RevisionClient
	ActBinary             string
	WorkDir               string
	LogDir                string
	ArtifactDir           string
	PollPeriod            time.Duration
	JobTimeout            time.Duration
	LeaseDuration         time.Duration
	LogMaxBytes           int64
	LogMaxLineBytes       int64
	ArtifactMaxCount      int
	ArtifactMaxFileBytes  int64
	ArtifactMaxTotalBytes int64
	ProxyURL              string
	EngineProxyURL        string
	ArtifactServerAddress string
}

const containerOptions = "--privileged=false --cpus=1 --memory=1g --pids-limit=256 " +
	"--cap-drop=ALL --security-opt=no-new-privileges:true"

const defaultArtifactServerAddress = "172.28.244.2"

const (
	maxJobTimeout    = 24 * time.Hour
	maxLogBytes      = int64(1 << 30)
	maxArtifactCount = 10_000
	maxArtifactFile  = int64(1 << 30)
	maxArtifactTotal = int64(2 << 30)
)

type Worker struct {
	store    *Store
	config   WorkerConfig
	workerID string
	logger   *slog.Logger
}

func NewWorker(store *Store, config WorkerConfig, logger *slog.Logger) (*Worker, error) {
	if config.Environment == "" {
		config.Environment = "production"
	}
	if config.Credentials != nil {
		if err := loreclient.ValidateServiceSubject(config.ServicePrincipal); err != nil {
			return nil, fmt.Errorf("Actions runner service principal is invalid: %w", err)
		}
		if config.RevisionClient == nil {
			return nil, errors.New("credential-aware Lore revision client is required")
		}
	} else {
		if config.Environment == "production" {
			return nil, errors.New("scoped Lore credential provider is required in production")
		}
		if config.CredentialIssuer == nil {
			return nil, errors.New("repository-scoped Lore credential issuer is required")
		}
	}
	if config.ExecutionResolver == nil {
		return nil, errors.New("Actions execution context resolver is required")
	}
	if config.JobTokenIssuer == nil {
		return nil, errors.New("Actions job token issuer is required")
	}
	if strings.TrimSpace(config.ActionSourceURL) == "" {
		config.ActionSourceURL = defaultActionSourceURL
	}
	if err := validateGitHubContext(config.GitHubContext, config.Environment); err != nil {
		return nil, err
	}
	if err := validateActionSourceURL(config.ActionSourceURL, config.Environment); err != nil {
		return nil, err
	}
	platformImages, err := mergedRunnerPlatformImages(config.PlatformImages)
	if err != nil {
		return nil, err
	}
	config.PlatformImages = platformImages
	if strings.TrimSpace(config.ProxyURL) == "" {
		return nil, errors.New("runner forward proxy URL is required")
	}
	if strings.TrimSpace(config.EngineProxyURL) == "" {
		return nil, errors.New("engine forward proxy URL is required")
	}
	if strings.TrimSpace(config.ArtifactServerAddress) == "" {
		config.ArtifactServerAddress = defaultArtifactServerAddress
	}
	if net.ParseIP(strings.TrimSpace(config.ArtifactServerAddress)) == nil {
		return nil, errors.New("artifact server address must be an IP address")
	}
	if config.JobTimeout <= 0 || config.JobTimeout > maxJobTimeout {
		return nil, errors.New("job timeout is outside its bounds")
	}
	if config.PollPeriod <= 0 {
		return nil, errors.New("runner poll period must be greater than zero")
	}
	for _, directory := range []string{config.WorkDir, config.LogDir, config.ArtifactDir} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			return nil, fmt.Errorf("create runner directory %q: %w", directory, err)
		}
	}
	binaries := map[string]string{"act": config.ActBinary}
	if config.RevisionClient == nil {
		binaries["Lore"] = config.LoreBinary
	}
	for name, binary := range binaries {
		if _, err := exec.LookPath(binary); err != nil {
			return nil, fmt.Errorf("%s executable %q is unavailable: %w", name, binary, err)
		}
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = 2 * time.Minute
	}
	if config.LeaseDuration >= config.JobTimeout {
		return nil, errors.New("job lease must be shorter than the job timeout")
	}
	if config.LogMaxBytes <= 0 {
		config.LogMaxBytes = 10 << 20
	}
	if config.LogMaxLineBytes <= 0 {
		config.LogMaxLineBytes = defaultMaskingLineBytes
	}
	if config.ArtifactMaxCount <= 0 {
		config.ArtifactMaxCount = 100
	}
	if config.ArtifactMaxFileBytes <= 0 {
		config.ArtifactMaxFileBytes = 100 << 20
	}
	if config.ArtifactMaxTotalBytes <= 0 {
		config.ArtifactMaxTotalBytes = 500 << 20
	}
	if config.LogMaxBytes > maxLogBytes || config.LogMaxLineBytes > config.LogMaxBytes ||
		config.ArtifactMaxCount > maxArtifactCount ||
		config.ArtifactMaxFileBytes > maxArtifactFile || config.ArtifactMaxTotalBytes > maxArtifactTotal ||
		config.ArtifactMaxFileBytes > config.ArtifactMaxTotalBytes {
		return nil, errors.New("runner log or artifact quota exceeds its bound")
	}
	return &Worker{
		store:    store,
		config:   config,
		workerID: uuid.NewString(),
		logger:   logger,
	}, nil
}

func (worker *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(worker.config.PollPeriod)
	defer ticker.Stop()
	for {
		job, err := worker.store.ClaimJob(ctx, worker.workerID, worker.config.LeaseDuration)
		if err != nil {
			worker.logger.Error("claim CI job", "error", err)
		} else if job != nil {
			worker.execute(ctx, *job)
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (worker *Worker) execute(parent context.Context, job Job) {
	ctx, cancel := context.WithTimeout(parent, worker.config.JobTimeout)
	defer cancel()
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		worker.monitorJob(ctx, job, cancel)
	}()

	logKey := worker.logObjectKey(job)
	conclusion := "success"
	logKey, artifacts, err := worker.runJob(ctx, job, logKey)
	if err != nil {
		conclusion = "failure"
		if errors.Is(err, context.DeadlineExceeded) {
			conclusion = "timed_out"
		} else if errors.Is(err, context.Canceled) {
			conclusion = "cancelled"
		}
		worker.logger.Error("CI job failed", "job_id", job.ID, "error", err)
	}
	cancel()
	<-monitorDone
	if parent.Err() != nil {
		if err := removeArtifactAttempt(worker.config.ArtifactDir, job); err != nil {
			worker.logger.Warn("remove abandoned CI artifacts", "job_id", job.ID, "error", err)
		}
		return
	}
	completeContext, completeCancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer completeCancel()
	if err := worker.store.CompleteJob(
		completeContext,
		job,
		worker.workerID,
		conclusion,
		logKey,
		artifacts,
	); err != nil {
		worker.logger.Error("record CI job completion", "job_id", job.ID, "error", err)
		if err := removeArtifactAttempt(worker.config.ArtifactDir, job); err != nil {
			worker.logger.Warn("remove uncommitted CI artifacts", "job_id", job.ID, "error", err)
		}
	}
}

func (worker *Worker) monitorJob(ctx context.Context, job Job, cancel context.CancelFunc) {
	heartbeatPeriod := worker.config.LeaseDuration / 3
	if heartbeatPeriod < time.Second {
		heartbeatPeriod = time.Second
	}
	heartbeatTicker := time.NewTicker(heartbeatPeriod)
	cancelTicker := time.NewTicker(500 * time.Millisecond)
	defer heartbeatTicker.Stop()
	defer cancelTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-cancelTicker.C:
			requested, err := worker.store.CancellationRequested(ctx, job.ID)
			if err != nil {
				worker.logger.Error("poll CI cancellation", "job_id", job.ID, "error", err)
				cancel()
				return
			}
			if requested {
				cancel()
				return
			}
		case <-heartbeatTicker.C:
			if err := worker.store.HeartbeatJob(ctx, job.ID, worker.workerID, worker.config.LeaseDuration); err != nil {
				worker.logger.Error("heartbeat CI job", "job_id", job.ID, "error", err)
				cancel()
				return
			}
		}
	}
}

func (worker *Worker) runJob(ctx context.Context, job Job, logKey string) (string, []Artifact, error) {
	if err := worker.removeStaleWorkspaces(job.ID); err != nil {
		return "", nil, err
	}
	workspace, err := os.MkdirTemp(worker.config.WorkDir, "job-"+job.ID+"-")
	if err != nil {
		return "", nil, fmt.Errorf("create CI workspace: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(workspace); err != nil {
			worker.logger.Warn("remove CI workspace", "path", workspace, "error", err)
		}
	}()

	logPath := filepath.Join(worker.config.LogDir, filepath.FromSlash(logKey))
	if err := os.MkdirAll(filepath.Dir(logPath), 0o750); err != nil {
		return "", nil, fmt.Errorf("create CI log directory: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", nil, fmt.Errorf("create CI log: %w", err)
	}
	defer func() { _ = logFile.Close() }()
	if err := worker.store.SetJobLogObjectKey(ctx, job, worker.workerID, logKey); err != nil {
		return "", nil, fmt.Errorf("publish CI log path: %w", err)
	}
	boundedWriter := &boundedLogWriter{writer: logFile, remaining: worker.config.LogMaxBytes}

	repositoryPath := filepath.Join(workspace, "repository")
	if err := worker.cloneRevision(ctx, job, repositoryPath, boundedWriter); err != nil {
		return logKey, nil, err
	}
	if err := removeLoreMetadata(repositoryPath); err != nil {
		return logKey, nil, err
	}
	if _, err := AdaptWorkflow(repositoryPath, job.WorkflowPath); err != nil {
		return logKey, nil, err
	}

	eventPath := filepath.Join(workspace, "event.json")
	if err := os.WriteFile(eventPath, job.EventPayload, 0o600); err != nil {
		return logKey, nil, fmt.Errorf("write CI event payload: %w", err)
	}
	artifactPath := filepath.Join(workspace, "artifacts")
	if err := os.MkdirAll(artifactPath, 0o750); err != nil {
		return logKey, nil, fmt.Errorf("create CI artifact directory: %w", err)
	}
	workflowPath := filepath.Join(repositoryPath, filepath.FromSlash(job.WorkflowPath))
	if err := validateWorkflowFile(workflowPath); err != nil {
		return logKey, nil, err
	}
	if err := validateWorkflowRunnerLabels(workflowPath, worker.config.PlatformImages); err != nil {
		return logKey, nil, err
	}
	workflowEnvironment, err := workflowEnvironmentName(workflowPath)
	if err != nil {
		return logKey, nil, err
	}
	if workflowEnvironment != job.Environment {
		return logKey, nil, errors.New("workflow environment does not match its approved deployment")
	}
	execution, err := resolveExecutionContext(ctx, worker.config.ExecutionResolver, ExecutionContextRequest{
		Principal:      worker.config.CredentialPrincipal,
		RepositoryID:   job.RepositoryID,
		OrganizationID: job.OrganizationID,
		JobID:          job.ID,
		Environment:    workflowEnvironment,
		RequestedScope: "actions:execute",
	})
	if err != nil {
		return logKey, nil, err
	}
	jobToken, err := issueJobToken(
		ctx,
		worker.config.JobTokenIssuer,
		job,
		worker.config.CredentialPrincipal,
		worker.config.JobTokenRESTScope,
		worker.config.JobTokenGraphQLScope,
	)
	if err != nil {
		return logKey, nil, err
	}
	execution.Secrets["GITHUB_TOKEN"] = jobToken.Token
	secretFile, err := writeSecretFile(workspace, execution.Secrets)
	if err != nil {
		return logKey, nil, err
	}
	defer cleanupSecretFile(secretFile)
	actContext, cancelAct := context.WithCancel(ctx)
	defer cancelAct()
	logWriter := newMaskingLogWriterWithLimit(
		boundedWriter,
		execution.Secrets,
		int(worker.config.LogMaxLineBytes),
		func(error) { cancelAct() },
	)
	actionDirectory := filepath.Join(workspace, "remote-actions")
	actionRepositories, err := prepareRemoteActions(
		ctx,
		workflowPath,
		actionDirectory,
		worker.config.ActionSourceURL,
		worker.config.Environment,
	)
	if err != nil {
		return logKey, nil, err
	}
	jobNetwork, err := worker.createJobNetwork(ctx, job.ID)
	if err != nil {
		return logKey, nil, err
	}
	defer cleanupJobNetwork(jobNetwork)
	arguments := actArguments(
		job,
		repositoryPath,
		workflowPath,
		eventPath,
		artifactPath,
		jobNetwork.networkName,
		jobNetwork.proxyURL,
		actInvocation{
			ActionRepositories:    actionRepositories,
			PlatformImages:        worker.config.PlatformImages,
			GitHub:                worker.config.GitHubContext,
			Variables:             execution.Variables,
			SecretFile:            secretFile,
			ArtifactServerAddress: worker.config.ArtifactServerAddress,
		},
	)
	act := exec.CommandContext(actContext, worker.config.ActBinary, arguments...)
	act.Stdout = logWriter
	act.Stderr = logWriter
	act.Env = append(safeEnvironment(), "LOREHUB_LORE_REVISION="+job.Revision)
	runErr := runAct(actContext, act)
	flushErr := logWriter.Flush()
	if maskErr := logWriter.Err(); maskErr != nil {
		runErr = fmt.Errorf("persist masked Actions log: %w", maskErr)
	} else if flushErr != nil && runErr == nil {
		runErr = fmt.Errorf("flush masked Actions log: %w", flushErr)
	}
	_ = logFile.Sync()
	artifacts, artifactErr := worker.persistArtifacts(job, artifactPath)
	if artifactErr != nil {
		return logKey, nil, artifactErr
	}
	if runErr != nil {
		return logKey, artifacts, fmt.Errorf("execute GitHub Actions workflow with act: %w", runErr)
	}
	return logKey, artifacts, nil
}

func (worker *Worker) removeStaleWorkspaces(jobID string) error {
	entries, err := os.ReadDir(worker.config.WorkDir)
	if err != nil {
		return fmt.Errorf("list stale CI workspaces: %w", err)
	}
	prefix := "job-" + jobID + "-"
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		path := filepath.Join(worker.config.WorkDir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove stale CI workspace %q: %w", path, err)
		}
	}
	return nil
}

func actArguments(
	job Job,
	repositoryPath string,
	workflowPath string,
	eventPath string,
	artifactPath string,
	networkName string,
	proxyURL string,
	config actInvocation,
) []string {
	if strings.TrimSpace(config.ArtifactServerAddress) == "" {
		config.ArtifactServerAddress = defaultArtifactServerAddress
	}
	arguments := []string{
		job.EventName,
		"--directory", repositoryPath,
		"--workflows", workflowPath,
		"--eventpath", eventPath,
		"--artifact-server-path", artifactPath,
		"--artifact-server-addr", config.ArtifactServerAddress,
		"--container-daemon-socket", "-",
		"--container-architecture", "linux/amd64",
		"--network", networkName,
		"--no-cache-server",
		"--rm",
		"--env", "LOREHUB_LORE_REVISION=" + job.Revision,
		"--env", "SHA_REF=" + job.Revision,
		"--env", "GITHUB_SHA=" + job.Revision,
		"--env", "GITHUB_REF=" + githubRef(job),
		"--env", "GITHUB_REPOSITORY=" + job.Owner + "/" + job.Repository,
		"--env", "GITHUB_EVENT_NAME=" + job.EventName,
		"--env", "GITHUB_SERVER_URL=" + config.GitHub.ServerURL,
		"--env", "GITHUB_API_URL=" + config.GitHub.APIURL,
		"--env", "GITHUB_GRAPHQL_URL=" + config.GitHub.GraphQLURL,
	}
	labels := make([]string, 0, len(config.PlatformImages))
	for label := range config.PlatformImages {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		arguments = append(arguments, "--platform", label+"="+config.PlatformImages[label])
	}
	for _, repository := range config.ActionRepositories {
		arguments = append(arguments, "--local-repository", repository)
	}
	arguments = append(arguments, "--container-options", containerOptions)
	if config.SecretFile != "" {
		arguments = append(arguments, "--secret-file", config.SecretFile)
	}
	for _, variable := range sortedVariables(config.Variables) {
		arguments = append(arguments, "--var", variable)
	}
	for _, variable := range []string{
		"HTTP_PROXY=" + proxyURL,
		"HTTPS_PROXY=" + proxyURL,
		"http_proxy=" + proxyURL,
		"https_proxy=" + proxyURL,
		"NO_PROXY=",
		"no_proxy=",
	} {
		arguments = append(arguments, "--env", variable)
	}
	return arguments
}

type actInvocation struct {
	ActionRepositories    []string
	PlatformImages        map[string]string
	GitHub                GitHubContext
	Variables             map[string]string
	SecretFile            string
	ArtifactServerAddress string
}

func sortedVariables(variables map[string]string) []string {
	keys := make([]string, 0, len(variables))
	for name := range variables {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, name := range keys {
		values = append(values, name+"="+variables[name])
	}
	return values
}

func runAct(ctx context.Context, act *exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := act.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- act.Wait() }()
	select {
	case err := <-done:
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	case <-ctx.Done():
		_ = act.Process.Signal(syscall.SIGTERM)
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
			return ctx.Err()
		case <-timer.C:
			_ = act.Process.Kill()
			<-done
			return ctx.Err()
		}
	}
}

func (worker *Worker) cloneRevision(ctx context.Context, job Job, destination string, output io.Writer) error {
	if worker.config.Credentials != nil {
		ref := loreclient.RepositoryRef{
			CacheKey: job.RepositoryID, URL: job.LoreURL, LoreRepositoryID: job.LoreRepositoryID,
		}
		partition, err := ref.ValidatedPartition()
		if err != nil {
			return fmt.Errorf("validate repository Lore boundary: %w", err)
		}
		credential, err := worker.config.Credentials.ForRepository(ctx, loreclient.CredentialRequest{
			Principal: loreclient.ServicePrincipal(
				loreclient.ServicePurposeActionsRunner,
				worker.config.ServicePrincipal,
			),
			Repository: ref,
			Partition:  partition,
			Scope:      loreclient.ScopeRead,
		})
		if err != nil {
			return errors.New("could not mint scoped Lore credential for CI job")
		}
		switch client := worker.config.RevisionClient.(type) {
		case loreclient.CredentialRevisionClient:
			err = client.CloneRevisionWithCredential(ctx, ref, credential, job.Revision, destination)
		case workflowRevisionClient:
			err = client.CloneWithCredential(ctx, job.LoreURL, job.Revision, destination, credential)
		default:
			return errors.New("configured Lore revision client does not accept scoped credentials")
		}
		if err != nil {
			return fmt.Errorf("clone Lore revision was rejected: %w", err)
		}
		return nil
	}

	credential, err := issueLoreCredential(
		ctx,
		worker.config.CredentialIssuer,
		worker.config.CredentialPrincipal,
		job.RepositoryID,
		job.LoreURL,
	)
	if err != nil {
		return fmt.Errorf("read repository Lore credential: %w", err)
	}
	if worker.config.RevisionClient != nil {
		repository := loreclient.RepositoryRef{
			CacheKey: job.RepositoryID, URL: job.LoreURL, LoreRepositoryID: job.LoreRepositoryID,
		}
		if client, ok := worker.config.RevisionClient.(loreclient.CredentialRevisionClient); ok {
			partition := loreclient.RepositoryRef{URL: job.LoreURL}.CanonicalPartition()
			if partition == "" {
				partition = job.RepositoryID
			}
			err = client.CloneRevisionWithCredential(ctx, repository,
				loreCredential(credential, worker.config.CredentialPrincipal, partition,
					issuerIsDevelopmentOnly(worker.config.CredentialIssuer)), job.Revision, destination)
		} else if credential.Identity != "" &&
			(worker.config.Environment == "development" || worker.config.Environment == "local") &&
			issuerIsDevelopmentOnly(worker.config.CredentialIssuer) {
			err = worker.config.RevisionClient.CloneRevision(
				ctx, repository, credential.Identity, job.Revision, destination,
			)
		} else {
			err = errors.New("the configured Lore client does not accept production Lore credentials")
		}
		if err != nil {
			return fmt.Errorf("clone Lore revision: %w", err)
		}
		return nil
	}
	clone := exec.CommandContext(
		ctx,
		worker.config.LoreBinary,
		"clone",
		"--revision",
		job.Revision,
		job.LoreURL,
		destination,
	)
	clone.Stdout = output
	clone.Stderr = output
	clone.Env = safeEnvironment()
	if err := clone.Run(); err != nil {
		return fmt.Errorf("clone Lore revision: %w", err)
	}
	return nil
}

func loreCredential(
	credential LoreCredential,
	principal CredentialPrincipal,
	partition string,
	insecureDevelopment bool,
) loreclient.Credential {
	return loreclient.Credential{
		Partition:           partition,
		Scope:               loreclient.ScopeRead,
		Principal:           loreclient.ServicePrincipal(loreclient.ServicePurposeActionsRunner, principal.Subject),
		Token:               credential.Token,
		AuthURL:             credential.AuthURL,
		Identity:            credential.Identity,
		ExpiresAt:           credential.ExpiresAt,
		InsecureDevelopment: insecureDevelopment,
	}
}

func githubRef(job Job) string {
	var event struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(job.EventPayload, &event); err == nil && strings.TrimSpace(event.Ref) != "" {
		if strings.HasPrefix(event.Ref, "refs/") {
			return event.Ref
		}
	}
	if strings.HasPrefix(job.Branch, "refs/") {
		return job.Branch
	}
	return "refs/heads/" + job.Branch
}

func removeLoreMetadata(repositoryPath string) error {
	metadataPath := filepath.Join(repositoryPath, ".lore")
	if err := os.RemoveAll(metadataPath); err != nil {
		return fmt.Errorf("remove Lore metadata before the job: %w", err)
	}
	return nil
}

func (worker *Worker) persistArtifacts(job Job, sourceDirectory string) ([]Artifact, error) {
	attemptName := fmt.Sprintf("attempt-%d", job.Attempt)
	destinationRoot := filepath.Join(worker.config.ArtifactDir, job.RepositoryID, job.RunID, job.ID, attemptName)
	stagingRoot := destinationRoot + ".partial-" + uuid.NewString()
	artifacts := make([]Artifact, 0)
	var totalBytes int64
	if err := os.MkdirAll(stagingRoot, 0o750); err != nil {
		return nil, fmt.Errorf("create CI artifact staging directory: %w", err)
	}
	err := filepath.WalkDir(sourceDirectory, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("CI artifact %q is a symbolic link", sourcePath)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("CI artifact %q is not a regular file", sourcePath)
		}
		if len(artifacts) >= worker.config.ArtifactMaxCount {
			return errors.New("CI artifact count quota exceeded")
		}
		if info.Size() > worker.config.ArtifactMaxFileBytes {
			return fmt.Errorf("CI artifact %q exceeds the per-file quota", sourcePath)
		}
		if info.Size() > worker.config.ArtifactMaxTotalBytes-totalBytes {
			return errors.New("CI artifact total-size quota exceeded")
		}
		relative, err := filepath.Rel(sourceDirectory, sourcePath)
		if err != nil || filepath.IsAbs(relative) || relative == ".." ||
			strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("CI artifact path escapes the artifact directory: %q", sourcePath)
		}
		destination, err := safeArtifactPath(stagingRoot, relative)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
			return err
		}
		copiedSize, err := copyArtifact(sourcePath, destination, worker.config.ArtifactMaxFileBytes)
		if err != nil {
			return err
		}
		if copiedSize > worker.config.ArtifactMaxTotalBytes-totalBytes {
			_ = os.Remove(destination)
			return errors.New("CI artifact total-size quota exceeded while copying")
		}
		objectKey := filepath.ToSlash(filepath.Join(job.RepositoryID, job.RunID, job.ID, attemptName, relative))
		artifacts = append(artifacts, Artifact{Name: filepath.ToSlash(relative), ObjectKey: objectKey, Size: copiedSize})
		totalBytes += copiedSize
		return nil
	})
	if err != nil {
		_ = os.RemoveAll(stagingRoot)
		return nil, fmt.Errorf("persist CI artifacts: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destinationRoot), 0o750); err != nil {
		_ = os.RemoveAll(stagingRoot)
		return nil, err
	}
	if err := os.Rename(stagingRoot, destinationRoot); err != nil {
		_ = os.RemoveAll(stagingRoot)
		return nil, fmt.Errorf("publish CI artifacts: %w", err)
	}
	return artifacts, nil
}

func copyArtifact(sourcePath string, destinationPath string, maxBytes int64) (int64, error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return 0, err
	}
	defer func() { _ = source.Close() }()
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return 0, err
	}
	copyLimit := maxBytes
	if copyLimit < int64(^uint64(0)>>1) {
		copyLimit++
	}
	size, copyErr := io.Copy(destination, io.LimitReader(source, copyLimit))
	closeErr := destination.Close()
	if copyErr != nil {
		_ = os.Remove(destinationPath)
		return 0, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destinationPath)
		return 0, closeErr
	}
	if size > maxBytes {
		_ = os.Remove(destinationPath)
		return 0, errors.New("CI artifact exceeded the per-file quota while copying")
	}
	return size, nil
}

func safeArtifactPath(root string, relative string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(root, relative))
	if err != nil || path != root && !strings.HasPrefix(path, root+string(filepath.Separator)) {
		return "", errors.New("CI artifact path escapes its destination")
	}
	return path, nil
}

func removeArtifactAttempt(root string, job Job) error {
	path := filepath.Join(root, job.RepositoryID, job.RunID, job.ID, fmt.Sprintf("attempt-%d", job.Attempt))
	return os.RemoveAll(path)
}

func validateWorkflowFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat selected workflow: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("selected workflow is not a regular file")
	}
	return nil
}

func (worker *Worker) logObjectKey(job Job) string {
	return filepath.ToSlash(filepath.Join(job.RepositoryID, job.RunID, job.ID, fmt.Sprintf("attempt-%d.log", job.Attempt)))
}

func safeEnvironment() []string {
	allowed := []string{
		"PATH",
		"HOME",
		"DOCKER_HOST",
		"DOCKER_TLS_VERIFY",
		"DOCKER_CERT_PATH",
		"DOCKER_CONFIG",
		"XDG_RUNTIME_DIR",
		"TMPDIR",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"http_proxy",
		"https_proxy",
		"NO_PROXY",
		"no_proxy",
	}
	environment := make([]string, 0, len(allowed))
	for _, key := range allowed {
		if value := os.Getenv(key); value != "" {
			environment = append(environment, key+"="+value)
		}
	}
	return environment
}

type boundedLogWriter struct {
	mu        sync.Mutex
	writer    io.Writer
	remaining int64
	marked    bool
}

const logLimitMarker = "\n[LoreHub log limit reached; remaining output was discarded]\n"

func (writer *boundedLogWriter) Write(contents []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.marked {
		return len(contents), nil
	}
	if int64(len(contents)) <= writer.remaining {
		written, err := writer.writer.Write(contents)
		writer.remaining -= int64(written)
		return len(contents), err
	}
	markerLimit := int64(len(logLimitMarker))
	contentLimit := writer.remaining - markerLimit
	if contentLimit < 0 {
		contentLimit = 0
	}
	written, err := writer.writer.Write(contents[:contentLimit])
	writer.remaining -= int64(written)
	if err != nil {
		return written, err
	}
	writer.writeMarker()
	return len(contents), nil
}

func (writer *boundedLogWriter) writeMarker() {
	if writer.marked {
		return
	}
	writer.marked = true
	if writer.remaining <= 0 {
		return
	}
	marker := logLimitMarker
	if int64(len(marker)) > writer.remaining {
		marker = marker[:writer.remaining]
	}
	written, _ := io.WriteString(writer.writer, marker)
	writer.remaining -= int64(written)
}
