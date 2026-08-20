package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

func (s *SQLiteStore) CreateMission(ctx context.Context, d *domain.MissionRequest) error {
	ex := s.executor(ctx)
	_, err := ex.Exec(`
		INSERT INTO mission_requests
			(id, robot_name, robot_serial, fleet_code, planned_start_at, route_preference,
			 mission_kind, estimated_energy_units, charge_mode, requested_by, requesting_party,
			 status, priority, queue_position, idempotency_key, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.RobotName, d.RobotSerial, d.FleetCode,
		d.PlannedStartAt.Format("2006-01-02T15:04:05Z07:00"), d.RoutePreference,
		d.MissionKind, d.EstimatedEnergyUnits, d.ChargeMode, d.RequestedBy, string(d.RequestingParty),
		string(d.Status), d.Priority, d.QueuePosition, d.IdempotencyKey,
		d.Version, d.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), d.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	)
	if err != nil {
		return fmt.Errorf("insert mission: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetMission(ctx context.Context, id string) (*domain.MissionRequest, error) {
	ex := s.executor(ctx)
	row := ex.QueryRow(`SELECT id, robot_name, robot_serial, fleet_code, planned_start_at, route_preference,
		mission_kind, estimated_energy_units, charge_mode, requested_by, requesting_party, status, priority,
		queue_position, idempotency_key, version, created_at, updated_at
		FROM mission_requests WHERE id = ?`, id)
	d, err := scanMission(row)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (s *SQLiteStore) ListMissions(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.MissionRequest], error) {
	ex := s.executor(ctx)
	params := PageParams{
		PageSize: q.PageSize,
		Offset:   q.Offset(),
		Filters:  q.Filter,
	}
	query, args := buildListQuery(`SELECT id, robot_name, robot_serial, fleet_code, planned_start_at, route_preference,
		mission_kind, estimated_energy_units, charge_mode, requested_by, requesting_party, status, priority,
		queue_position, idempotency_key, version, created_at, updated_at
		FROM mission_requests`, params)
	rows, err := ex.Query(query, args...)
	if err != nil {
		return domain.PageResult[*domain.MissionRequest]{}, fmt.Errorf("list missions: %w", err)
	}
	defer rows.Close()
	var items []*domain.MissionRequest
	for rows.Next() {
		d, err := scanMission(rows)
		if err != nil {
			return domain.PageResult[*domain.MissionRequest]{}, err
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[*domain.MissionRequest]{}, err
	}
	total, err := s.countFiltered(ctx, "mission_requests", q.Filter)
	if err != nil {
		return domain.PageResult[*domain.MissionRequest]{}, err
	}
	return domain.NewPageResult(items, total, q.Page, q.PageSize), nil
}

func (s *SQLiteStore) UpdateMissionStatus(ctx context.Context, id string, status domain.MissionStatus, version int) (int, error) {
	ex := s.executor(ctx)
	res, err := ex.Exec(`UPDATE mission_requests SET status = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`,
		string(status), nowStamp(), id, version)
	if err != nil {
		return 0, fmt.Errorf("update mission status: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) UpdateQueuePosition(ctx context.Context, id string, pos, version int) (int, error) {
	ex := s.executor(ctx)
	res, err := ex.Exec(`UPDATE mission_requests SET queue_position = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`, pos, nowStamp(), id, version)
	if err != nil {
		return 0, fmt.Errorf("update queue position: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) UpdatePriority(ctx context.Context, id string, priority, version int) (int, error) {
	ex := s.executor(ctx)
	res, err := ex.Exec(`UPDATE mission_requests SET priority = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`, priority, nowStamp(), id, version)
	if err != nil {
		return 0, fmt.Errorf("update priority: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) CountMissionsByStatus(ctx context.Context, status domain.MissionStatus) (int, error) {
	ex := s.executor(ctx)
	var n int
	err := ex.QueryRow("SELECT COUNT(*) FROM mission_requests WHERE status = ?", string(status)).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (s *SQLiteStore) ListMissionsByStatus(ctx context.Context, status domain.MissionStatus) ([]*domain.MissionRequest, error) {
	ex := s.executor(ctx)
	rows, err := ex.Query(`SELECT id, robot_name, robot_serial, fleet_code, planned_start_at, route_preference,
		mission_kind, estimated_energy_units, charge_mode, requested_by, requesting_party, status, priority,
		queue_position, idempotency_key, version, created_at, updated_at
		FROM mission_requests WHERE status = ? ORDER BY priority ASC, created_at ASC`, string(status))
	if err != nil {
		return nil, fmt.Errorf("list missions by status: %w", err)
	}
	defer rows.Close()
	var items []*domain.MissionRequest
	for rows.Next() {
		d, err := scanMission(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

func (s *SQLiteStore) GetMissionByIdempotencyKey(ctx context.Context, key string) (*domain.MissionRequest, error) {
	if key == "" {
		return nil, domain.NewNotFoundError("mission", "")
	}
	ex := s.executor(ctx)
	row := ex.QueryRow(`SELECT id, robot_name, robot_serial, fleet_code, planned_start_at, route_preference,
		mission_kind, estimated_energy_units, charge_mode, requested_by, requesting_party, status, priority,
		queue_position, idempotency_key, version, created_at, updated_at
		FROM mission_requests WHERE idempotency_key = ?`, key)
	d, err := scanMission(row)
	if err != nil {
		return nil, err
	}
	return d, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanMission(sc scanner) (*domain.MissionRequest, error) {
	d := &domain.MissionRequest{}
	var plannedStartAt, createdAt, updatedAt string
	var partyStr string
	var statusStr string
	err := sc.Scan(
		&d.ID, &d.RobotName, &d.RobotSerial, &d.FleetCode, &plannedStartAt, &d.RoutePreference,
		&d.MissionKind, &d.EstimatedEnergyUnits, &d.ChargeMode, &d.RequestedBy, &partyStr,
		&statusStr, &d.Priority, &d.QueuePosition, &d.IdempotencyKey,
		&d.Version, &createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.NewNotFoundError("mission", "")
		}
		return nil, err
	}
	d.PlannedStartAt = parseTime(plannedStartAt)
	d.RequestingParty = domain.PartyRole(partyStr)
	d.Status = domain.MissionStatus(statusStr)
	d.CreatedAt = parseTime(createdAt)
	d.UpdatedAt = parseTime(updatedAt)
	return d, nil
}

func (s *SQLiteStore) countFiltered(ctx context.Context, table string, filters map[string]string) (int, error) {
	ex := s.executor(ctx)
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	var args []any
	if len(filters) > 0 {
		query += " WHERE "
		clauses := make([]string, 0, len(filters))
		for col, val := range filters {
			clauses = append(clauses, fmt.Sprintf("%s = ?", col))
			args = append(args, val)
		}
		query += joinAnd(clauses)
	}
	var n int
	err := ex.QueryRow(query, args...).Scan(&n)
	return n, err
}
