package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

func (s *SQLiteStore) InsertAudit(ctx context.Context, entry *domain.AuditLog) error {
	ex := s.executor(ctx)
	_, err := ex.Exec(`
		INSERT INTO audit_logs (id, actor, action, entity_type, entity_id, before_state, after_state, request_id, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.Actor, entry.Action,
		string(entry.EntityType), entry.EntityID,
		entry.BeforeState, entry.AfterState, entry.RequestID,
		entry.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListAuditLogs(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.AuditLog], error) {
	ex := s.executor(ctx)
	params := PageParams{PageSize: q.PageSize, Offset: q.Offset(), Filters: q.Filter, OrderBy: "timestamp", OrderDir: "DESC"}
	query, args := buildListQuery(`SELECT id, actor, action, entity_type, entity_id, before_state, after_state, request_id, timestamp
		FROM audit_logs`, params)
	rows, err := ex.Query(query, args...)
	if err != nil {
		return domain.PageResult[*domain.AuditLog]{}, fmt.Errorf("list audit logs: %w", err)
	}
	defer rows.Close()
	var items []*domain.AuditLog
	for rows.Next() {
		a, err := scanAudit(rows)
		if err != nil {
			return domain.PageResult[*domain.AuditLog]{}, err
		}
		items = append(items, a)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[*domain.AuditLog]{}, err
	}
	total, err := s.countFiltered(ctx, "audit_logs", q.Filter)
	if err != nil {
		return domain.PageResult[*domain.AuditLog]{}, err
	}
	return domain.NewPageResult(items, total, q.Page, q.PageSize), nil
}

func scanAudit(sc scanner) (*domain.AuditLog, error) {
	a := &domain.AuditLog{}
	var etStr, tsStr string
	err := sc.Scan(
		&a.ID, &a.Actor, &a.Action, &etStr, &a.EntityID,
		&a.BeforeState, &a.AfterState, &a.RequestID, &tsStr,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.NewNotFoundError("audit_log", "")
		}
		return nil, err
	}
	a.EntityType = domain.EntityType(etStr)
	a.Timestamp = parseTime(tsStr)
	return a, nil
}

func (s *SQLiteStore) ListAuditByEntity(ctx context.Context, entityType domain.EntityType, entityID string) ([]*domain.AuditLog, error) {
	ex := s.executor(ctx)
	rows, err := ex.Query(`SELECT id, actor, action, entity_type, entity_id, before_state, after_state, request_id, timestamp
		FROM audit_logs WHERE entity_type = ? AND entity_id = ? ORDER BY timestamp ASC`, string(entityType), entityID)
	if err != nil {
		return nil, fmt.Errorf("list audit by entity: %w", err)
	}
	defer rows.Close()
	var items []*domain.AuditLog
	for rows.Next() {
		a, err := scanAudit(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}
