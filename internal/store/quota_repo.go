package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

func (s *SQLiteStore) GetOrCreateQuota(ctx context.Context, qt domain.QuotaType, date string, limit int) (*domain.Quota, error) {
	ex := s.executor(ctx)
	// Try to fetch first.
	q, err := s.GetQuotaByTypeDate(ctx, qt, date)
	if err == nil {
		return q, nil
	}
	if !domain.IsNotFound(err) {
		return nil, err
	}
	// Insert new quota row.
	q = &domain.Quota{
		ID:             newID(),
		QuotaType:      qt,
		PeriodDate:     date,
		DailyLimit:     limit,
		UsedAmount:     0,
		ReservedAmount: 0,
		Status:         domain.QuotaStatusAvailable,
		Version:        1,
		CreatedAt:      timeNow(),
		UpdatedAt:      timeNow(),
	}
	_, err = ex.Exec(`INSERT INTO quotas (id, quota_type, period_date, daily_limit, used_amount, reserved_amount, status, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		q.ID, string(qt), date, limit, 0, 0, string(domain.QuotaStatusAvailable),
		q.Version, q.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), q.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"))
	if err != nil {
		// Race: another goroutine inserted first. Re-fetch.
		q2, err2 := s.GetQuotaByTypeDate(ctx, qt, date)
		if err2 == nil {
			return q2, nil
		}
		return nil, fmt.Errorf("insert quota: %w", err)
	}
	return q, nil
}

func (s *SQLiteStore) GetQuota(ctx context.Context, id string) (*domain.Quota, error) {
	ex := s.executor(ctx)
	row := ex.QueryRow(`SELECT id, quota_type, period_date, daily_limit, used_amount, reserved_amount, status, version, created_at, updated_at
		FROM quotas WHERE id = ?`, id)
	return scanQuota(row)
}

func (s *SQLiteStore) GetQuotaByTypeDate(ctx context.Context, qt domain.QuotaType, date string) (*domain.Quota, error) {
	ex := s.executor(ctx)
	row := ex.QueryRow(`SELECT id, quota_type, period_date, daily_limit, used_amount, reserved_amount, status, version, created_at, updated_at
		FROM quotas WHERE quota_type = ? AND period_date = ?`, string(qt), date)
	return scanQuota(row)
}

func scanQuota(sc scanner) (*domain.Quota, error) {
	q := &domain.Quota{}
	var typeStr, statusStr, createdAt, updatedAt string
	err := sc.Scan(
		&q.ID, &typeStr, &q.PeriodDate, &q.DailyLimit, &q.UsedAmount,
		&q.ReservedAmount, &statusStr, &q.Version, &createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.NewNotFoundError("quota", "")
		}
		return nil, err
	}
	q.QuotaType = domain.QuotaType(typeStr)
	q.Status = domain.QuotaStatus(statusStr)
	q.CreatedAt = parseTime(createdAt)
	q.UpdatedAt = parseTime(updatedAt)
	return q, nil
}

func (s *SQLiteStore) ListQuotas(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.Quota], error) {
	ex := s.executor(ctx)
	params := PageParams{PageSize: q.PageSize, Offset: q.Offset(), Filters: q.Filter}
	query, args := buildListQuery(`SELECT id, quota_type, period_date, daily_limit, used_amount, reserved_amount, status, version, created_at, updated_at
		FROM quotas`, params)
	rows, err := ex.Query(query, args...)
	if err != nil {
		return domain.PageResult[*domain.Quota]{}, fmt.Errorf("list quotas: %w", err)
	}
	defer rows.Close()
	var items []*domain.Quota
	for rows.Next() {
		q, err := scanQuota(rows)
		if err != nil {
			return domain.PageResult[*domain.Quota]{}, err
		}
		items = append(items, q)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[*domain.Quota]{}, err
	}
	total, err := s.countFiltered(ctx, "quotas", q.Filter)
	if err != nil {
		return domain.PageResult[*domain.Quota]{}, err
	}
	return domain.NewPageResult(items, total, q.Page, q.PageSize), nil
}

func (s *SQLiteStore) ReserveQuota(ctx context.Context, id string, amount, version int) (int, error) {
	ex := s.executor(ctx)
	res, err := ex.Exec(`UPDATE quotas SET reserved_amount = reserved_amount + ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ? AND daily_limit - used_amount - reserved_amount >= ?`,
		amount, nowStamp(), id, version, amount)
	if err != nil {
		return 0, fmt.Errorf("reserve quota: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) CommitQuota(ctx context.Context, id string, amount, version int) (int, error) {
	ex := s.executor(ctx)
	res, err := ex.Exec(`UPDATE quotas SET used_amount = used_amount + ?, reserved_amount = reserved_amount - ?,
		version = version + 1, updated_at = ? WHERE id = ? AND version = ? AND reserved_amount >= ?`,
		amount, amount, nowStamp(), id, version, amount)
	if err != nil {
		return 0, fmt.Errorf("commit quota: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) ReleaseQuota(ctx context.Context, id string, amount, version int) (int, error) {
	ex := s.executor(ctx)
	res, err := ex.Exec(`UPDATE quotas SET reserved_amount = reserved_amount - ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ? AND reserved_amount >= ?`,
		amount, nowStamp(), id, version, amount)
	if err != nil {
		return 0, fmt.Errorf("release quota: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) ListAllQuotas(ctx context.Context) ([]*domain.Quota, error) {
	ex := s.executor(ctx)
	rows, err := ex.Query(`SELECT id, quota_type, period_date, daily_limit, used_amount, reserved_amount, status, version, created_at, updated_at
		FROM quotas ORDER BY period_date DESC, quota_type ASC`)
	if err != nil {
		return nil, fmt.Errorf("list all quotas: %w", err)
	}
	defer rows.Close()
	var items []*domain.Quota
	for rows.Next() {
		q, err := scanQuota(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, q)
	}
	return items, rows.Err()
}
