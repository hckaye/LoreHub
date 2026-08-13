package runnerclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type Daemon struct {
	client     *Client
	executor   Executor
	pollPeriod time.Duration
	jobTimeout time.Duration
	logger     *slog.Logger
}

func NewDaemon(
	client *Client,
	executor Executor,
	pollPeriod time.Duration,
	jobTimeout time.Duration,
	logger *slog.Logger,
) (*Daemon, error) {
	if client == nil || executor == nil {
		return nil, errors.New("runner client and executor are required")
	}
	if pollPeriod <= 0 {
		pollPeriod = 2 * time.Second
	}
	if jobTimeout <= 0 {
		jobTimeout = 6 * time.Hour
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Daemon{
		client: client, executor: executor, pollPeriod: pollPeriod, jobTimeout: jobTimeout, logger: logger,
	}, nil
}

func (daemon *Daemon) Run(ctx context.Context) error {
	ticker := time.NewTicker(daemon.pollPeriod)
	defer ticker.Stop()
	for {
		claimed, err := daemon.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			daemon.logger.Error("run self-hosted CI job", "error", err)
		}
		if claimed {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (daemon *Daemon) RunOnce(ctx context.Context) (bool, error) {
	claim, err := daemon.client.Claim(ctx)
	if err != nil {
		return false, err
	}
	if claim.Job == nil {
		return false, nil
	}
	jobContext, cancel := context.WithTimeout(ctx, daemon.jobTimeout)
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		daemon.monitor(jobContext, claim, cancel)
	}()
	result, executionErr := daemon.executor.Execute(jobContext, *claim.Job, daemon.client)
	if executionErr != nil {
		result = executionFailure(nil, executionErr)
	}
	if result.Conclusion == "" {
		result.Conclusion = "failure"
	}
	cancel()
	<-monitorDone
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	completionContext, completionCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer completionCancel()
	if err := daemon.client.UploadLog(completionContext, claim.Job.ID, result.Log); err != nil {
		return true, fmt.Errorf("upload runner job log: %w", err)
	}
	for _, artifact := range result.Artifacts {
		if err := daemon.client.UploadArtifact(
			completionContext, claim.Job.ID, artifact.Name, artifact.Content,
		); err != nil {
			return true, fmt.Errorf("upload runner job artifact %q: %w", artifact.Name, err)
		}
	}
	if err := daemon.client.Complete(completionContext, claim.Job.ID, result.Conclusion); err != nil {
		return true, fmt.Errorf("complete runner job: %w", err)
	}
	return true, nil
}

func (daemon *Daemon) monitor(ctx context.Context, claim Claim, cancel context.CancelFunc) {
	lease := LeaseDuration(claim)
	heartbeatPeriod := lease / 3
	if heartbeatPeriod < time.Second {
		heartbeatPeriod = time.Second
	}
	heartbeatTicker := time.NewTicker(heartbeatPeriod)
	cancellationTicker := time.NewTicker(500 * time.Millisecond)
	defer heartbeatTicker.Stop()
	defer cancellationTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeatTicker.C:
			if err := daemon.client.Heartbeat(ctx, claim.Job.ID); err != nil {
				daemon.logger.Error("heartbeat self-hosted CI job", "job_id", claim.Job.ID, "error", err)
				cancel()
				return
			}
		case <-cancellationTicker.C:
			requested, err := daemon.client.CancellationRequested(ctx, claim.Job.ID)
			if err != nil {
				daemon.logger.Error("poll self-hosted CI cancellation", "job_id", claim.Job.ID, "error", err)
				cancel()
				return
			}
			if requested {
				cancel()
				return
			}
		}
	}
}
