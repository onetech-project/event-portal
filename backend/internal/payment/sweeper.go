package payment

import (
	"context"
	"time"
)

// Sweeper periodically expires orders whose payment deadline has passed.
//
// It is a plain in-process ticker, not a scheduler or a queue: the MVP rules
// those out (Constitution Principle VI), and the job is small enough that a
// ticker is the honest amount of machinery. Its only reason to exist is that
// quota held by an abandoned order must come back even when nobody opens the
// page and no provider notification arrives.
type Sweeper struct {
	svc      *Service
	interval time.Duration
}

// NewSweeper builds the expiry sweeper.
func NewSweeper(svc *Service, interval time.Duration) *Sweeper {
	return &Sweeper{svc: svc, interval: interval}
}

// Run sweeps until ctx is cancelled. It is meant to be started in its own
// goroutine and stopped by cancelling that context during shutdown.
func (s *Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepOnce(ctx)
		}
	}
}

func (s *Sweeper) sweepOnce(ctx context.Context) {
	expired, err := s.svc.ExpireDueOrders(ctx)
	if err != nil {
		s.svc.log.ErrorContext(ctx, "expiry sweep failed", "error", err.Error())
		return
	}
	if expired > 0 {
		// Only worth a line when something actually happened: a quiet sweep every
		// 30 seconds would drown the log.
		s.svc.log.InfoContext(ctx, "expiry sweep completed", "orders_expired", expired)
	}
}
