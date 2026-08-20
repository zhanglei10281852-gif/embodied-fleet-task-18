package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

func (s *SQLiteStore) windowCreate(ctx context.Context, w *domain.RouteReservation) error {
	ex := s.executor(ctx)
	_, err := ex.Exec(`
		INSERT INTO route_reservations
			(id, mission_id, corridor_id, robot_name, effective_at, deadline_at,
			 assigned_to, responsible_party, escalation_level, status, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.MissionID, w.CorridorID, w.RobotName,
		w.EffectiveAt.Format("2006-01-02T15:04:05Z07:00"), w.DeadlineAt.Format("2006-01-02T15:04:05Z07:00"),
		w.AssignedTo, string(w.ResponsibleParty), w.EscalationLevel, string(w.Status),
		w.Version, w.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), w.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	)
	if err != nil {
		return fmt.Errorf("insert window: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CreateReservation(ctx context.Context, w *domain.RouteReservation) error {
	return s.windowCreate(ctx, w)
}

func (s *SQLiteStore) CreateReservationsBatch(ctx context.Context, ws []*domain.RouteReservation) error {
	for _, w := range ws {
		if err := s.windowCreate(ctx, w); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) GetReservation(ctx context.Context, id string) (*domain.RouteReservation, error) {
	ex := s.executor(ctx)
	row := ex.QueryRow(`SELECT id, mission_id, corridor_id, robot_name, effective_at, deadline_at,
		assigned_to, responsible_party, escalation_level, status, version, created_at, updated_at
		FROM route_reservations WHERE id = ?`, id)
	return scanWindow(row)
}

func scanWindow(sc scanner) (*domain.RouteReservation, error) {
	w := &domain.RouteReservation{}
	var effectiveAt, deadlineAt, createdAt, updatedAt, partyStr, statusStr string
	err := sc.Scan(
		&w.ID, &w.MissionID, &w.CorridorID, &w.RobotName,
		&effectiveAt, &deadlineAt, &w.AssignedTo, &partyStr,
		&w.EscalationLevel, &statusStr, &w.Version, &createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.NewNotFoundError("route_reservation", "")
		}
		return nil, err
	}
	w.EffectiveAt = parseTime(effectiveAt)
	w.DeadlineAt = parseTime(deadlineAt)
	w.ResponsibleParty = domain.PartyRole(partyStr)
	w.Status = domain.ReservationStatus(statusStr)
	w.CreatedAt = parseTime(createdAt)
	w.UpdatedAt = parseTime(updatedAt)
	return w, nil
}

func (s *SQLiteStore) ListReservations(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.RouteReservation], error) {
	ex := s.executor(ctx)
	params := PageParams{PageSize: q.PageSize, Offset: q.Offset(), Filters: q.Filter}
	query, args := buildListQuery(`SELECT id, mission_id, corridor_id, robot_name, effective_at, deadline_at,
		assigned_to, responsible_party, escalation_level, status, version, created_at, updated_at
		FROM route_reservations`, params)
	rows, err := ex.Query(query, args...)
	if err != nil {
		return domain.PageResult[*domain.RouteReservation]{}, fmt.Errorf("list windows: %w", err)
	}
	defer rows.Close()
	var items []*domain.RouteReservation
	for rows.Next() {
		w, err := scanWindow(rows)
		if err != nil {
			return domain.PageResult[*domain.RouteReservation]{}, err
		}
		items = append(items, w)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[*domain.RouteReservation]{}, err
	}
	total, err := s.countFiltered(ctx, "route_reservations", q.Filter)
	if err != nil {
		return domain.PageResult[*domain.RouteReservation]{}, err
	}
	return domain.NewPageResult(items, total, q.Page, q.PageSize), nil
}

func (s *SQLiteStore) UpdateReservationStatus(ctx context.Context, id string, status domain.ReservationStatus, version int) (int, error) {
	ex := s.executor(ctx)
	res, err := ex.Exec(`UPDATE route_reservations SET status = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`, string(status), nowStamp(), id, version)
	if err != nil {
		return 0, fmt.Errorf("update window status: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) UpdateReservationAssignedTo(ctx context.Context, id string, assignedTo string, level int, version int) (int, error) {
	ex := s.executor(ctx)
	res, err := ex.Exec(`UPDATE route_reservations SET assigned_to = ?, escalation_level = ?, status = 'escalated',
		version = version + 1, updated_at = ? WHERE id = ? AND version = ?`,
		assignedTo, level, nowStamp(), id, version)
	if err != nil {
		return 0, fmt.Errorf("escalate window: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) ListExpiredWindows(ctx context.Context, now string) ([]*domain.RouteReservation, error) {
	ex := s.executor(ctx)
	rows, err := ex.Query(`SELECT id, mission_id, corridor_id, robot_name, effective_at, deadline_at,
		assigned_to, responsible_party, escalation_level, status, version, created_at, updated_at
		FROM route_reservations WHERE deadline_at < ? AND status IN ('effective', 'occupied', 'escalated')
		ORDER BY deadline_at ASC`, now)
	if err != nil {
		return nil, fmt.Errorf("list expired windows: %w", err)
	}
	defer rows.Close()
	var items []*domain.RouteReservation
	for rows.Next() {
		w, err := scanWindow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, w)
	}
	return items, rows.Err()
}

func (s *SQLiteStore) ListReservationsByStatus(ctx context.Context, status domain.ReservationStatus) ([]*domain.RouteReservation, error) {
	ex := s.executor(ctx)
	rows, err := ex.Query(`SELECT id, mission_id, corridor_id, robot_name, effective_at, deadline_at,
		assigned_to, responsible_party, escalation_level, status, version, created_at, updated_at
		FROM route_reservations WHERE status = ? ORDER BY deadline_at ASC`, string(status))
	if err != nil {
		return nil, fmt.Errorf("list windows by status: %w", err)
	}
	defer rows.Close()
	var items []*domain.RouteReservation
	for rows.Next() {
		w, err := scanWindow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, w)
	}
	return items, rows.Err()
}

func (s *SQLiteStore) CountReservationsByStatus(ctx context.Context, status domain.ReservationStatus) (int, error) {
	ex := s.executor(ctx)
	var n int
	err := ex.QueryRow("SELECT COUNT(*) FROM route_reservations WHERE status = ?", string(status)).Scan(&n)
	return n, err
}
