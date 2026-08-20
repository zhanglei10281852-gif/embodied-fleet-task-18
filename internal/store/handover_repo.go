package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

func (s *SQLiteStore) CreateHandover(ctx context.Context, h *domain.MaintenanceHandover) error {
	ex := s.executor(ctx)
	_, err := ex.Exec(`
		INSERT INTO maintenance_handovers
			(id, entity_type, entity_id, from_party, to_party, action, document_ref, status, notes, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.ID, string(h.EntityType), h.EntityID,
		string(h.FromParty), string(h.ToParty), h.Action, h.DocumentRef,
		string(h.Status), h.Notes, h.Version,
		h.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), h.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	)
	if err != nil {
		return fmt.Errorf("insert handover: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetHandover(ctx context.Context, id string) (*domain.MaintenanceHandover, error) {
	ex := s.executor(ctx)
	row := ex.QueryRow(`SELECT id, entity_type, entity_id, from_party, to_party, action, document_ref, status, notes, version, created_at, updated_at
		FROM maintenance_handovers WHERE id = ?`, id)
	return scanHandover(row)
}

func scanHandover(sc scanner) (*domain.MaintenanceHandover, error) {
	h := &domain.MaintenanceHandover{}
	var etStr, fromStr, toStr, statusStr, createdAt, updatedAt string
	err := sc.Scan(
		&h.ID, &etStr, &h.EntityID, &fromStr, &toStr, &h.Action,
		&h.DocumentRef, &statusStr, &h.Notes, &h.Version, &createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.NewNotFoundError("handover_document", "")
		}
		return nil, err
	}
	h.EntityType = domain.EntityType(etStr)
	h.FromParty = domain.PartyRole(fromStr)
	h.ToParty = domain.PartyRole(toStr)
	h.Status = domain.HandoverStatus(statusStr)
	h.CreatedAt = parseTime(createdAt)
	h.UpdatedAt = parseTime(updatedAt)
	return h, nil
}

func (s *SQLiteStore) ListHandovers(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.MaintenanceHandover], error) {
	ex := s.executor(ctx)
	params := PageParams{PageSize: q.PageSize, Offset: q.Offset(), Filters: q.Filter}
	query, args := buildListQuery(`SELECT id, entity_type, entity_id, from_party, to_party, action, document_ref, status, notes, version, created_at, updated_at
		FROM maintenance_handovers`, params)
	rows, err := ex.Query(query, args...)
	if err != nil {
		return domain.PageResult[*domain.MaintenanceHandover]{}, fmt.Errorf("list handovers: %w", err)
	}
	defer rows.Close()
	var items []*domain.MaintenanceHandover
	for rows.Next() {
		h, err := scanHandover(rows)
		if err != nil {
			return domain.PageResult[*domain.MaintenanceHandover]{}, err
		}
		items = append(items, h)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[*domain.MaintenanceHandover]{}, err
	}
	total, err := s.countFiltered(ctx, "maintenance_handovers", q.Filter)
	if err != nil {
		return domain.PageResult[*domain.MaintenanceHandover]{}, err
	}
	return domain.NewPageResult(items, total, q.Page, q.PageSize), nil
}

func (s *SQLiteStore) UpdateHandoverStatus(ctx context.Context, id string, status domain.HandoverStatus, version int) (int, error) {
	ex := s.executor(ctx)
	res, err := ex.Exec(`UPDATE maintenance_handovers SET status = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`, string(status), nowStamp(), id, version)
	if err != nil {
		return 0, fmt.Errorf("update handover status: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) ListHandoversByEntity(ctx context.Context, entityType domain.EntityType, entityID string) ([]*domain.MaintenanceHandover, error) {
	ex := s.executor(ctx)
	rows, err := ex.Query(`SELECT id, entity_type, entity_id, from_party, to_party, action, document_ref, status, notes, version, created_at, updated_at
		FROM maintenance_handovers WHERE entity_type = ? AND entity_id = ? ORDER BY created_at ASC`, string(entityType), entityID)
	if err != nil {
		return nil, fmt.Errorf("list handovers by entity: %w", err)
	}
	defer rows.Close()
	var items []*domain.MaintenanceHandover
	for rows.Next() {
		h, err := scanHandover(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, h)
	}
	return items, rows.Err()
}
