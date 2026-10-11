package interfaces

import (
	"context"
	"time"

	"go.uber.org/zap"

	app "nidaw-backend/internal/modules/notifications/application"
)

// RetryWorker periodically re-attempts due fcm/email notification rows with
// the dispatcher's exponential-backoff policy (§1.4: 3 attempts, then
// terminal failure). In-app rows never appear here — they are synchronous and
// complete at dispatch time.
//
// The worker is safe to run on multiple replicas: PendingRetries selects only
// rows whose next_retry_at has elapsed, and each retry either marks the row
// delivered or pushes next_retry_at forward, so concurrent replicas converge
// without a lock. Worst case is one duplicate push during a race window,
// which the client dedupes by notification_id.
type RetryWorker struct {
	dispatcher *app.Dispatcher
	interval   time.Duration
	logger     *zap.Logger // may be nil
}

// NewRetryWorker constructs the worker. interval <= 0 falls back to 30s.
func NewRetryWorker(d *app.Dispatcher, interval time.Duration, logger *zap.Logger) *RetryWorker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &RetryWorker{dispatcher: d, interval: interval, logger: logger}
}

// Run loops until ctx is cancelled. Each tick processes all due rows once;
// errors are logged, never fatal — the DB state is the source of truth and
// the next tick retries naturally.
func (w *RetryWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := w.dispatcher.RetryDue(ctx)
			if err != nil {
				if w.logger != nil {
					w.logger.Error("notification retry pass failed", zap.Error(err))
				}
				continue
			}
			if n > 0 && w.logger != nil {
				w.logger.Info("notification retry pass", zap.Int("rows_processed", n))
			}
		}
	}
}
