package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

func (s *SQLiteStore) CreateLease(ctx context.Context, l *domain.TaskLease) error {
	ex := s.executor(ctx)
	_, err := ex.Exec(`
		INSERT INTO task_leases
			(id, task_type, task_id, executor_id, claimed_at, expires_at, status, revoked_reason, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.ID, string(l.TaskType), l.TaskID, l.ExecutorID,
		l.ClaimedAt.Format("2006-01-02T15:04:05Z07:00"), l.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
		string(l.Status), l.RevokedReason, l.Version,
		l.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), l.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	)
	if err != nil {
		return fmt.Errorf("insert lease: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetLease(ctx context.Context, id string) (*domain.TaskLease, error) {
	ex := s.executor(ctx)
	row := ex.QueryRow(`SELECT id, task_type, task_id, executor_id, claimed_at, expires_at, status, revoked_reason, version, created_at, updated_at
		FROM task_leases WHERE id = ?`, id)
	return scanLease(row)
}

func scanLease(sc scanner) (*domain.TaskLease, error) {
	l := &domain.TaskLease{}
	var typeStr, statusStr, claimedAt, expiresAt, createdAt, updatedAt string
	err := sc.Scan(
		&l.ID, &typeStr, &l.TaskID, &l.ExecutorID, &claimedAt, &expiresAt,
		&statusStr, &l.RevokedReason, &l.Version, &createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.NewNotFoundError("task_lease", "")
		}
		return nil, err
	}
	l.TaskType = domain.EntityType(typeStr)
	l.Status = domain.LeaseStatus(statusStr)
	l.ClaimedAt = parseTime(claimedAt)
	l.ExpiresAt = parseTime(expiresAt)
	l.CreatedAt = parseTime(createdAt)
	l.UpdatedAt = parseTime(updatedAt)
	return l, nil
}

func (s *SQLiteStore) GetActiveLeaseByTask(ctx context.Context, taskType domain.EntityType, taskID string) (*domain.TaskLease, error) {
	ex := s.executor(ctx)
	row := ex.QueryRow(`SELECT id, task_type, task_id, executor_id, claimed_at, expires_at, status, revoked_reason, version, created_at, updated_at
		FROM task_leases WHERE task_type = ? AND task_id = ? AND status = 'active' ORDER BY created_at DESC LIMIT 1`,
		string(taskType), taskID)
	return scanLease(row)
}

func (s *SQLiteStore) RevokeLease(ctx context.Context, id, reason string, version int) (int, error) {
	ex := s.executor(ctx)
	res, err := ex.Exec(`UPDATE task_leases SET status = 'revoked', revoked_reason = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ? AND status = 'active'`, reason, nowStamp(), id, version)
	if err != nil {
		return 0, fmt.Errorf("revoke lease: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) ListExpiredLeases(ctx context.Context, now string) ([]*domain.TaskLease, error) {
	ex := s.executor(ctx)
	rows, err := ex.Query(`SELECT id, task_type, task_id, executor_id, claimed_at, expires_at, status, revoked_reason, version, created_at, updated_at
		FROM task_leases WHERE status = 'active' AND expires_at < ? ORDER BY expires_at ASC`, now)
	if err != nil {
		return nil, fmt.Errorf("list expired leases: %w", err)
	}
	defer rows.Close()
	var items []*domain.TaskLease
	for rows.Next() {
		l, err := scanLease(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, l)
	}
	return items, rows.Err()
}

func (s *SQLiteStore) ReleaseLease(ctx context.Context, id string, version int) (int, error) {
	ex := s.executor(ctx)
	res, err := ex.Exec(`UPDATE task_leases SET status = 'released', version = version + 1, updated_at = ?
		WHERE id = ? AND version = ? AND status = 'active'`, nowStamp(), id, version)
	if err != nil {
		return 0, fmt.Errorf("release lease: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
