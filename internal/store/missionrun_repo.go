package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

func (s *SQLiteStore) CreateMissionRun(ctx context.Context, w *domain.MissionRun) error {
	ex := s.executor(ctx)
	_, err := ex.Exec(`
		INSERT INTO mission_runs
			(id, mission_id, route_reservation_id, run_type, mission_kind, planned_units,
			 actual_units, assigned_to, status, started_at, completed_at, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.MissionID, nullString(w.RouteReservationID), string(w.RunType),
		w.MissionKind, w.PlannedUnits, w.ActualUnits, w.AssignedTo, string(w.Status),
		nullTime(w.StartedAt), nullTime(w.CompletedAt),
		w.Version, w.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), w.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	)
	if err != nil {
		return fmt.Errorf("insert mission run: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetMissionRun(ctx context.Context, id string) (*domain.MissionRun, error) {
	ex := s.executor(ctx)
	row := ex.QueryRow(`SELECT id, mission_id, route_reservation_id, run_type, mission_kind, planned_units,
		actual_units, assigned_to, status, started_at, completed_at, version, created_at, updated_at
		FROM mission_runs WHERE id = ?`, id)
	return scanMissionRun(row)
}

func scanMissionRun(sc scanner) (*domain.MissionRun, error) {
	w := &domain.MissionRun{}
	var bwID sql.NullString
	var startedAt, completedAt sql.NullString
	var createdAt, updatedAt, typeStr, statusStr string
	err := sc.Scan(
		&w.ID, &w.MissionID, &bwID, &typeStr, &w.MissionKind, &w.PlannedUnits,
		&w.ActualUnits, &w.AssignedTo, &statusStr, &startedAt, &completedAt,
		&w.Version, &createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.NewNotFoundError("mission_run", "")
		}
		return nil, err
	}
	w.RouteReservationID = parseNullString(bwID)
	w.StartedAt = parseNullTime(startedAt)
	w.CompletedAt = parseNullTime(completedAt)
	w.RunType = domain.MissionRunType(typeStr)
	w.Status = domain.MissionRunStatus(statusStr)
	w.CreatedAt = parseTime(createdAt)
	w.UpdatedAt = parseTime(updatedAt)
	return w, nil
}

func (s *SQLiteStore) ListMissionRuns(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.MissionRun], error) {
	ex := s.executor(ctx)
	params := PageParams{PageSize: q.PageSize, Offset: q.Offset(), Filters: q.Filter}
	query, args := buildListQuery(`SELECT id, mission_id, route_reservation_id, run_type, mission_kind,
		planned_units, actual_units, assigned_to, status, started_at, completed_at, version, created_at, updated_at
		FROM mission_runs`, params)
	rows, err := ex.Query(query, args...)
	if err != nil {
		return domain.PageResult[*domain.MissionRun]{}, fmt.Errorf("list mission runs: %w", err)
	}
	defer rows.Close()
	var items []*domain.MissionRun
	for rows.Next() {
		w, err := scanMissionRun(rows)
		if err != nil {
			return domain.PageResult[*domain.MissionRun]{}, err
		}
		items = append(items, w)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[*domain.MissionRun]{}, err
	}
	total, err := s.countFiltered(ctx, "mission_runs", q.Filter)
	if err != nil {
		return domain.PageResult[*domain.MissionRun]{}, err
	}
	return domain.NewPageResult(items, total, q.Page, q.PageSize), nil
}

func (s *SQLiteStore) UpdateMissionRunStatus(ctx context.Context, id string, status domain.MissionRunStatus, version int) (int, error) {
	ex := s.executor(ctx)
	var startedAtExpr, completedAtExpr string
	var args []any
	args = append(args, string(status))
	if status == domain.RunStatusInProgress {
		startedAtExpr = ", started_at = ?"
		args = append(args, nowStamp())
	}
	if status == domain.RunStatusCompleted {
		completedAtExpr = ", completed_at = ?"
		args = append(args, nowStamp())
	}
	args = append(args, nowStamp(), id, version)
	query := fmt.Sprintf(`UPDATE mission_runs SET status = ?%s%s, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`, startedAtExpr, completedAtExpr)
	res, err := ex.Exec(query, args...)
	if err != nil {
		return 0, fmt.Errorf("update mission run status: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) UpdateActualUnits(ctx context.Context, id string, volume, version int) (int, error) {
	ex := s.executor(ctx)
	res, err := ex.Exec(`UPDATE mission_runs SET actual_units = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`, volume, nowStamp(), id, version)
	if err != nil {
		return 0, fmt.Errorf("update actual volume: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) ListMissionRunsByStatus(ctx context.Context, status domain.MissionRunStatus) ([]*domain.MissionRun, error) {
	ex := s.executor(ctx)
	rows, err := ex.Query(`SELECT id, mission_id, route_reservation_id, run_type, mission_kind,
		planned_units, actual_units, assigned_to, status, started_at, completed_at, version, created_at, updated_at
		FROM mission_runs WHERE status = ? ORDER BY created_at ASC`, string(status))
	if err != nil {
		return nil, fmt.Errorf("list mission runs by status: %w", err)
	}
	defer rows.Close()
	var items []*domain.MissionRun
	for rows.Next() {
		w, err := scanMissionRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, w)
	}
	return items, rows.Err()
}

func (s *SQLiteStore) CountMissionRunsByStatus(ctx context.Context, status domain.MissionRunStatus) (int, error) {
	ex := s.executor(ctx)
	var n int
	err := ex.QueryRow("SELECT COUNT(*) FROM mission_runs WHERE status = ?", string(status)).Scan(&n)
	return n, err
}
