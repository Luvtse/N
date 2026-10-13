package interfaces

import (
	"context"
	"time"

	telephonyapp "nidaw-backend/internal/modules/telephony/application"
)

// Sweeper periodically expires sessions whose TTL elapsed without a terminal
// ride event (lost message, crashed producer). It is the inventory safety net:
// without it, proxy numbers leak and the pool eventually reports
// ErrNoProxyNumber, silently disabling masked calling.
type Sweeper struct {
	svc      *telephonyapp.Service
	interval time.Duration
}

// NewSweeper defaults to a 60-second cadence; expiry granularity at minute
// scale is plenty for phone-number recycling.
func NewSweeper(svc *telephonyapp.Service, interval time.Duration) *Sweeper {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Sweeper{svc: svc, interval: interval}
}

// Run blocks until ctx is cancelled, sweeping on every tick. Errors from
// individual sweeps are swallowed because the next tick retries; persistent
// failures surface through the pool's Available() metric dropping toward zero.
func (s *Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = s.svc.SweepExpired()
		}
	}
}
