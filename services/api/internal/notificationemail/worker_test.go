package notificationemail

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type workerTestStore struct {
	projected []int
	claims    []*platform.NotificationEmailClaim
	completed []bool
}

func (store *workerTestStore) ProjectNotifications(context.Context) (int, error) {
	if len(store.projected) == 0 {
		return 0, nil
	}
	projected := store.projected[0]
	store.projected = store.projected[1:]
	return projected, nil
}

func (store *workerTestStore) ClaimNotificationEmail(
	context.Context,
	string,
	time.Duration,
) (*platform.NotificationEmailClaim, error) {
	if len(store.claims) == 0 {
		return nil, nil
	}
	claim := store.claims[0]
	store.claims = store.claims[1:]
	return claim, nil
}

func (store *workerTestStore) CompleteNotificationEmail(
	_ context.Context,
	_ string,
	_ platform.NotificationEmailClaim,
	_ int,
	succeeded bool,
	_ time.Time,
	_ string,
) error {
	store.completed = append(store.completed, succeeded)
	return nil
}

type workerTestSender struct {
	err      error
	messages []Message
}

func (sender *workerTestSender) Send(_ context.Context, message Message) error {
	sender.messages = append(sender.messages, message)
	return sender.err
}

func TestWorkerProjectsAndCompletesDelivery(t *testing.T) {
	store := &workerTestStore{
		projected: []int{1, 0},
		claims: []*platform.NotificationEmailClaim{{
			DeliveryID: "delivery", NotificationID: "notification", Recipient: "alice@example.com",
			Locale: "en", Title: "Issue updated", Href: "/acme/game/issues/1", Attempt: 1,
		}},
	}
	sender := &workerTestSender{}
	worker := newWorkerForTest(t, store, sender)
	worker.runCycle(context.Background())
	if len(sender.messages) != 1 || len(store.completed) != 1 || !store.completed[0] {
		t.Fatalf("delivery was not completed: messages=%d completed=%v", len(sender.messages), store.completed)
	}
}

func TestWorkerRecordsFailedDelivery(t *testing.T) {
	store := &workerTestStore{claims: []*platform.NotificationEmailClaim{{
		DeliveryID: "delivery", NotificationID: "notification", Recipient: "alice@example.com",
		Locale: "en", Title: "Issue updated", Href: "/acme/game/issues/1", Attempt: 1,
	}}}
	sender := &workerTestSender{err: errors.New("SMTP unavailable")}
	worker := newWorkerForTest(t, store, sender)
	worker.runCycle(context.Background())
	if len(store.completed) != 1 || store.completed[0] {
		t.Fatalf("failed delivery was not recorded: %v", store.completed)
	}
}

func newWorkerForTest(t *testing.T, store Store, sender Sender) *Worker {
	t.Helper()
	worker, err := NewWorker(store, sender, Config{
		PollPeriod: time.Second, Lease: 15 * time.Second, SendTimeout: 5 * time.Second,
		MaxAttempts: 3, PublicOrigin: "https://lorehub.example",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return worker
}
