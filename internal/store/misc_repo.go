package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

// --- Escalation records ---

func (s *SQLiteStore) InsertEscalation(ctx context.Context, r *domain.EscalationRecord) error {
	ex := s.executor(ctx)
	_, err := ex.Exec(`
		INSERT INTO escalation_records (id, entity_type, entity_id, from_level, to_level, reason, resolved_by, resolved_at, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, string(r.EntityType), r.EntityID, r.FromLevel, r.ToLevel,
		r.Reason, r.ResolvedBy, nullTime(r.ResolvedAt),
		r.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	)
	if err != nil {
		return fmt.Errorf("insert escalation: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListEscalations(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.EscalationRecord], error) {
	return s.listEscalations(ctx, q)
}

func (s *SQLiteStore) listEscalations(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.EscalationRecord], error) {
	ex := s.executor(ctx)
	params := PageParams{PageSize: q.PageSize, Offset: q.Offset(), Filters: q.Filter, OrderBy: "timestamp", OrderDir: "DESC"}
	query, args := buildListQuery(`SELECT id, entity_type, entity_id, from_level, to_level, reason, resolved_by, resolved_at, timestamp
		FROM escalation_records`, params)
	rows, err := ex.Query(query, args...)
	if err != nil {
		return domain.PageResult[*domain.EscalationRecord]{}, fmt.Errorf("list escalations: %w", err)
	}
	defer rows.Close()
	var items []*domain.EscalationRecord
	for rows.Next() {
		r, err := scanEscalation(rows)
		if err != nil {
			return domain.PageResult[*domain.EscalationRecord]{}, err
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[*domain.EscalationRecord]{}, err
	}
	total, err := s.countFiltered(ctx, "escalation_records", q.Filter)
	if err != nil {
		return domain.PageResult[*domain.EscalationRecord]{}, err
	}
	return domain.NewPageResult(items, total, q.Page, q.PageSize), nil
}

func scanEscalation(sc scanner) (*domain.EscalationRecord, error) {
	r := &domain.EscalationRecord{}
	var etStr, tsStr string
	var resolvedBy sql.NullString
	var resolvedAt sql.NullString
	err := sc.Scan(
		&r.ID, &etStr, &r.EntityID, &r.FromLevel, &r.ToLevel,
		&r.Reason, &resolvedBy, &resolvedAt, &tsStr,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.NewNotFoundError("escalation_record", "")
		}
		return nil, err
	}
	r.EntityType = domain.EntityType(etStr)
	r.Timestamp = parseTime(tsStr)
	r.ResolvedBy = parseNullString(resolvedBy)
	r.ResolvedAt = parseNullTime(resolvedAt)
	return r, nil
}

func (s *SQLiteStore) ListEscalationsByEntity(ctx context.Context, entityType domain.EntityType, entityID string) ([]*domain.EscalationRecord, error) {
	ex := s.executor(ctx)
	rows, err := ex.Query(`SELECT id, entity_type, entity_id, from_level, to_level, reason, resolved_by, resolved_at, timestamp
		FROM escalation_records WHERE entity_type = ? AND entity_id = ? ORDER BY timestamp ASC`, string(entityType), entityID)
	if err != nil {
		return nil, fmt.Errorf("list escalations by entity: %w", err)
	}
	defer rows.Close()
	var items []*domain.EscalationRecord
	for rows.Next() {
		r, err := scanEscalation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	return items, rows.Err()
}

// --- Execution records ---

func (s *SQLiteStore) InsertExecution(ctx context.Context, r *domain.ExecutionRecord) error {
	ex := s.executor(ctx)
	_, err := ex.Exec(`
		INSERT INTO execution_records (id, task_type, task_id, executor_id, result, error_message, duration_ms, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, string(r.TaskType), r.TaskID, r.ExecutorID,
		r.Result, r.ErrorMessage, r.DurationMs,
		r.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	)
	if err != nil {
		return fmt.Errorf("insert execution record: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListExecutions(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.ExecutionRecord], error) {
	ex := s.executor(ctx)
	params := PageParams{PageSize: q.PageSize, Offset: q.Offset(), Filters: q.Filter, OrderBy: "timestamp", OrderDir: "DESC"}
	query, args := buildListQuery(`SELECT id, task_type, task_id, executor_id, result, error_message, duration_ms, timestamp
		FROM execution_records`, params)
	rows, err := ex.Query(query, args...)
	if err != nil {
		return domain.PageResult[*domain.ExecutionRecord]{}, fmt.Errorf("list execution records: %w", err)
	}
	defer rows.Close()
	var items []*domain.ExecutionRecord
	for rows.Next() {
		r := &domain.ExecutionRecord{}
		var etStr, tsStr string
		err := rows.Scan(&r.ID, &etStr, &r.TaskID, &r.ExecutorID, &r.Result, &r.ErrorMessage, &r.DurationMs, &tsStr)
		if err != nil {
			return domain.PageResult[*domain.ExecutionRecord]{}, err
		}
		r.TaskType = domain.EntityType(etStr)
		r.Timestamp = parseTime(tsStr)
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[*domain.ExecutionRecord]{}, err
	}
	total, err := s.countFiltered(ctx, "execution_records", q.Filter)
	if err != nil {
		return domain.PageResult[*domain.ExecutionRecord]{}, err
	}
	return domain.NewPageResult(items, total, q.Page, q.PageSize), nil
}

// --- Idempotency records ---

func (s *SQLiteStore) GetIdempotency(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	ex := s.executor(ctx)
	row := ex.QueryRow(`SELECT key, response_body, response_status, created_at, expires_at
		FROM idempotency_records WHERE key = ?`, key)
	r := &domain.IdempotencyRecord{}
	var createdAt, expiresAt string
	err := row.Scan(&r.Key, &r.ResponseBody, &r.ResponseStatus, &createdAt, &expiresAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.NewNotFoundError("idempotency_record", key)
		}
		return nil, err
	}
	r.CreatedAt = parseTime(createdAt)
	r.ExpiresAt = parseTime(expiresAt)
	return r, nil
}

func (s *SQLiteStore) InsertIdempotency(ctx context.Context, r *domain.IdempotencyRecord) error {
	ex := s.executor(ctx)
	_, err := ex.Exec(`INSERT INTO idempotency_records (key, response_body, response_status, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)`,
		r.Key, r.ResponseBody, r.ResponseStatus,
		r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), r.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"))
	if err != nil {
		return fmt.Errorf("insert idempotency record: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CleanExpiredIdempotency(ctx context.Context, now string) (int, error) {
	ex := s.executor(ctx)
	res, err := ex.Exec("DELETE FROM idempotency_records WHERE expires_at < ?", now)
	if err != nil {
		return 0, fmt.Errorf("clean expired idempotency: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
