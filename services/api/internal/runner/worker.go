package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/lore"
)

type WorkerConfig struct {
	LoreBinary       string
	ActBinary        string
	WorkDir          string
	LogDir           string
	ArtifactDir      string
	PollPeriod       time.Duration
	JobTimeout       time.Duration
	Lore             lore.CredentialClient
	Issuer           CredentialIssuer
	ServicePrincipal string
}

type Worker struct {
	store    *Store
	config   WorkerConfig
	workerID string
	logger   *slog.Logger
}

func NewWorker(store *Store, config WorkerConfig, logger *slog.Logger) (*Worker, error) {
	for _, directory := range []string{config.WorkDir, config.LogDir, config.ArtifactDir} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			return nil, fmt.Errorf("create runner directory %q: %w", directory, err)
		}
	}
	for name, binary := range map[string]string{"Lore": config.LoreBinary, "act": config.ActBinary} {
		if _, err := exec.LookPath(binary); err != nil {
			return nil, fmt.Errorf("%s executable %q is unavailable: %w", name, binary, err)
		}
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
		job, err := worker.store.ClaimJob(ctx, worker.workerID, worker.config.JobTimeout+time.Minute)
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
	conclusion := "success"
	logKey, artifacts, err := worker.runJob(ctx, job)
	if err != nil {
		conclusion = "failure"
		if errors.Is(err, context.DeadlineExceeded) {
			conclusion = "timed_out"
		}
		worker.logger.Error("CI job failed", "job_id", job.ID, "error", err)
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
	}
}

func (worker *Worker) runJob(ctx context.Context, job Job) (string, []Artifact, error) {
	workspace, err := os.MkdirTemp(worker.config.WorkDir, "job-"+job.ID+"-")
	if err != nil {
		return "", nil, fmt.Errorf("create CI workspace: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(workspace); err != nil {
			worker.logger.Warn("remove CI workspace", "path", workspace, "error", err)
		}
	}()

	attemptName := fmt.Sprintf("attempt-%d", job.Attempt)
	logKey := filepath.Join(job.RepositoryID, job.RunID, job.ID, attemptName+".log")
	logPath := filepath.Join(worker.config.LogDir, logKey)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o750); err != nil {
		return "", nil, fmt.Errorf("create CI log directory: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return "", nil, fmt.Errorf("create CI log: %w", err)
	}
	defer func() { _ = logFile.Close() }()

	repositoryPath := filepath.Join(workspace, "repository")
	if worker.config.Lore == nil || worker.config.Issuer == nil || worker.config.ServicePrincipal == "" {
		return logKey, nil, errors.New("CI runner requires scoped Lore credentials")
	}
	token, err := worker.config.Issuer.IssueServiceResourceToken(ctx, worker.config.ServicePrincipal,
		"urc-"+job.LoreRepositoryID, []string{"read"})
	if err != nil {
		return logKey, nil, errors.New("could not mint CI Lore credential")
	}
	if err := worker.config.Lore.CloneWithCredential(ctx, job.LoreURL, job.Revision, repositoryPath, lore.Credential{
		Token: token, AuthURL: worker.config.Issuer.AuthURL(), Identity: worker.config.ServicePrincipal,
	}); err != nil {
		return logKey, nil, errors.New("clone Lore revision was rejected")
	}
	if _, err := AdaptWorkflows(repositoryPath); err != nil {
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
	workflowPath := filepath.Join(repositoryPath, ".github", "workflows")
	act := exec.CommandContext(
		ctx,
		worker.config.ActBinary,
		job.EventName,
		"--directory",
		repositoryPath,
		"--workflows",
		workflowPath,
		"--eventpath",
		eventPath,
		"--artifact-server-path",
		artifactPath,
	)
	act.Stdout = io.MultiWriter(logFile)
	act.Stderr = io.MultiWriter(logFile)
	act.Env = safeEnvironment()
	runErr := act.Run()
	artifacts, artifactErr := worker.persistArtifacts(job, artifactPath)
	if artifactErr != nil {
		return logKey, nil, artifactErr
	}
	if runErr != nil {
		return logKey, artifacts, fmt.Errorf("execute GitHub Actions workflow with act: %w", runErr)
	}
	return logKey, artifacts, nil
}

func (worker *Worker) persistArtifacts(job Job, sourceDirectory string) ([]Artifact, error) {
	attemptName := fmt.Sprintf("attempt-%d", job.Attempt)
	destinationRoot := filepath.Join(worker.config.ArtifactDir, job.RepositoryID, job.RunID, job.ID, attemptName)
	artifacts := make([]Artifact, 0)
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
		relative, err := filepath.Rel(sourceDirectory, sourcePath)
		if err != nil {
			return fmt.Errorf("resolve CI artifact path: %w", err)
		}
		destination := filepath.Join(destinationRoot, relative)
		if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
			return err
		}
		size, err := copyArtifact(sourcePath, destination)
		if err != nil {
			return err
		}
		objectKey := filepath.ToSlash(
			filepath.Join(job.RepositoryID, job.RunID, job.ID, attemptName, relative),
		)
		artifacts = append(artifacts, Artifact{Name: relative, ObjectKey: objectKey, Size: size})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("persist CI artifacts: %w", err)
	}
	return artifacts, nil
}

func copyArtifact(sourcePath string, destinationPath string) (int64, error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return 0, err
	}
	defer func() { _ = source.Close() }()
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return 0, err
	}
	size, copyErr := io.Copy(destination, source)
	closeErr := destination.Close()
	if copyErr != nil {
		return 0, copyErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	return size, nil
}

func safeEnvironment() []string {
	allowed := []string{"PATH", "HOME", "DOCKER_HOST", "DOCKER_CONFIG", "XDG_RUNTIME_DIR", "TMPDIR"}
	environment := make([]string, 0, len(allowed))
	for _, key := range allowed {
		if value := os.Getenv(key); value != "" {
			environment = append(environment, key+"="+value)
		}
	}
	return environment
}
