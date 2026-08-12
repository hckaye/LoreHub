package notificationemail

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const (
	maxDeliveriesPerCycle = 25
	maxProjectionBatches  = 10
)

type Store interface {
	ProjectNotifications(context.Context) (int, error)
	ClaimNotificationEmail(context.Context, string, time.Duration) (*platform.NotificationEmailClaim, error)
	CompleteNotificationEmail(
		context.Context,
		string,
		platform.NotificationEmailClaim,
		int,
		bool,
		time.Time,
		string,
	) error
}

type Sender interface {
	Send(context.Context, Message) error
}

type Config struct {
	PollPeriod   time.Duration
	Lease        time.Duration
	SendTimeout  time.Duration
	MaxAttempts  int
	PublicOrigin string
}

type Worker struct {
	store    Store
	sender   Sender
	config   Config
	workerID string
	logger   *slog.Logger
}

func NewWorker(store Store, sender Sender, config Config, logger *slog.Logger) (*Worker, error) {
	if store == nil || sender == nil {
		return nil, errors.New("notification email store and sender are required")
	}
	if config.PollPeriod <= 0 || config.PollPeriod > time.Minute {
		return nil, errors.New("notification email poll period must be no longer than one minute")
	}
	if config.SendTimeout <= 0 || config.SendTimeout > time.Minute {
		return nil, errors.New("notification email send timeout must be no longer than one minute")
	}
	if config.Lease < config.SendTimeout+5*time.Second || config.Lease > 5*time.Minute {
		return nil, errors.New("notification email lease must cover the send timeout")
	}
	if config.MaxAttempts < 1 || config.MaxAttempts > 20 {
		return nil, errors.New("notification email max attempts must be between 1 and 20")
	}
	origin, err := url.Parse(config.PublicOrigin)
	if err != nil || origin.Scheme == "" || origin.Host == "" || origin.Path != "" {
		return nil, errors.New("notification email public origin is invalid")
	}
	config.PublicOrigin = strings.TrimRight(config.PublicOrigin, "/")
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		store: store, sender: sender, config: config, workerID: uuid.NewString(), logger: logger,
	}, nil
}

func (worker *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(worker.config.PollPeriod)
	defer ticker.Stop()
	for {
		worker.runCycle(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (worker *Worker) runCycle(ctx context.Context) {
	for index := 0; index < maxProjectionBatches; index++ {
		projected, err := worker.store.ProjectNotifications(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				worker.logger.Error("Notification projection failed", "error", err)
			}
			return
		}
		if projected == 0 {
			break
		}
	}
	for index := 0; index < maxDeliveriesPerCycle; index++ {
		claim, err := worker.store.ClaimNotificationEmail(ctx, worker.workerID, worker.config.Lease)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				worker.logger.Error("Notification email claim failed", "error", err)
			}
			return
		}
		if claim == nil {
			return
		}
		worker.deliver(ctx, *claim)
	}
}

func (worker *Worker) deliver(ctx context.Context, claim platform.NotificationEmailClaim) {
	message := RenderMessage(claim, worker.config.PublicOrigin)
	sendContext, cancel := context.WithTimeout(ctx, worker.config.SendTimeout)
	err := worker.sender.Send(sendContext, message)
	cancel()
	retryAt := time.Now().UTC()
	if err != nil {
		retryAt = retryAt.Add(retryDelay(claim.Attempt))
		message := "Notification email will be retried"
		if claim.Attempt >= worker.config.MaxAttempts {
			message = "Notification email attempts exhausted"
		}
		worker.logger.Warn(
			message,
			"delivery_id",
			claim.DeliveryID,
			"attempt",
			claim.Attempt,
			"error",
			err,
		)
	}
	if completeErr := worker.store.CompleteNotificationEmail(
		ctx,
		worker.workerID,
		claim,
		worker.config.MaxAttempts,
		err == nil,
		retryAt,
		errorText(err),
	); completeErr != nil && !errors.Is(completeErr, context.Canceled) {
		worker.logger.Error(
			"Notification email completion failed",
			"delivery_id",
			claim.DeliveryID,
			"error",
			completeErr,
		)
	}
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := 30 * time.Second
	for current := 1; current < attempt && delay < time.Hour; current++ {
		delay *= 2
	}
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
