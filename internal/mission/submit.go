package mission

import (
	"context"
	"fmt"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

// reserveQuotaWithRetry attempts to reserve quota, retrying on optimistic-lock
// conflicts up to maxRetries times. This is necessary because concurrent
// submissions all read and reserve the same quota row, and version conflicts
// are expected.
func (s *Service) reserveQuotaWithRetry(ctx context.Context, quotaType domain.QuotaType,
	dateStr string, limit, amount, maxRetries int) (reserved bool, quotaID string, err error) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		q, err := s.quota.GetOrCreateQuota(ctx, quotaType, dateStr, limit)
		if err != nil {
			return false, "", apperr.Wrap(apperr.CodeInternal, "quota lookup failed", err)
		}
		if q.Available() < amount {
			return false, q.ID, nil
		}
		affected, err := s.quota.ReserveQuota(ctx, q.ID, amount, q.Version)
		if err != nil {
			if store.IsRetryableContention(err) && attempt < maxRetries {
				if err := waitForContention(ctx, attempt); err != nil {
					return false, q.ID, err
				}
				continue
			}
			return false, q.ID, apperr.Wrap(apperr.CodeConflict, "quota reserve failed", err)
		}
		if affected > 0 {
			return true, q.ID, nil
		}
		// Version conflict — retry with fresh read.
	}
	return false, "", fmt.Errorf("quota reserve failed after %d retries", maxRetries)
}

func waitForContention(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(attempt+1) * 2 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
