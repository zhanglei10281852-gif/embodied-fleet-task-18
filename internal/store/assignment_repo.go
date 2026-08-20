package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

func (s *SQLiteStore) CreateAssignment(ctx context.Context, t *domain.RobotAssignment) error {
	ex := s.executor(ctx)
	_, err := ex.Exec(`
		INSERT INTO robot_assignments
			(id, mission_id, route_reservation_id, task_type, location, assigned_to,
			 claimed_by, claim_expires_at, lease_id, status, priority, report_data,
			 version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.MissionID, nullString(t.RouteReservationID),
		string(t.TaskType), t.Location, t.AssignedTo,
		t.ClaimedBy, nullTime(t.ClaimExpiresAt), nullString(t.LeaseID),
		string(t.Status), t.Priority, t.ReportData,
		t.Version, t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	)
	if err != nil {
		return fmt.Errorf("insert robot assignment: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetAssignment(ctx context.Context, id string) (*domain.RobotAssignment, error) {
	ex := s.executor(ctx)
	row := ex.QueryRow(`SELECT id, mission_id, route_reservation_id, task_type, location, assigned_to,
		claimed_by, claim_expires_at, lease_id, status, priority, report_data, version, created_at, updated_at
		FROM robot_assignments WHERE id = ?`, id)
	return scanAssignment(row)
}

func scanAssignment(sc scanner) (*domain.RobotAssignment, error) {
	t := &domain.RobotAssignment{}
	var bwID, claimExpires, leaseID sql.NullString
	var createdAt, updatedAt, typeStr, statusStr string
	err := sc.Scan(
		&t.ID, &t.MissionID, &bwID, &typeStr, &t.Location, &t.AssignedTo,
		&t.ClaimedBy, &claimExpires, &leaseID, &statusStr, &t.Priority, &t.ReportData,
		&t.Version, &createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.NewNotFoundError("robot_assignment", "")
		}
		return nil, err
	}
	t.RouteReservationID = parseNullString(bwID)
	t.ClaimExpiresAt = parseNullTime(claimExpires)
	t.LeaseID = parseNullString(leaseID)
	t.TaskType = domain.AssignmentType(typeStr)
	t.Status = domain.AssignmentStatus(statusStr)
	t.CreatedAt = parseTime(createdAt)
	t.UpdatedAt = parseTime(updatedAt)
	return t, nil
}

func (s *SQLiteStore) ListAssignments(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.RobotAssignment], error) {
	ex := s.executor(ctx)
	params := PageParams{PageSize: q.PageSize, Offset: q.Offset(), Filters: q.Filter}
	query, args := buildListQuery(`SELECT id, mission_id, route_reservation_id, task_type, location, assigned_to,
		claimed_by, claim_expires_at, lease_id, status, priority, report_data, version, created_at, updated_at
		FROM robot_assignments`, params)
	rows, err := ex.Query(query, args...)
	if err != nil {
		return domain.PageResult[*domain.RobotAssignment]{}, fmt.Errorf("list robot assignments: %w", err)
	}
	defer rows.Close()
	var items []*domain.RobotAssignment
	for rows.Next() {
		t, err := scanAssignment(rows)
		if err != nil {
			return domain.PageResult[*domain.RobotAssignment]{}, err
		}
		items = append(items, t)
	}
	if err := rows.Err(); err != nil {
		return domain.PageResult[*domain.RobotAssignment]{}, err
	}
	total, err := s.countFiltered(ctx, "robot_assignments", q.Filter)
	if err != nil {
		return domain.PageResult[*domain.RobotAssignment]{}, err
	}
	return domain.NewPageResult(items, total, q.Page, q.PageSize), nil
}

func (s *SQLiteStore) UpdateAssignmentStatus(ctx context.Context, id string, status domain.AssignmentStatus, version int) (int, error) {
	ex := s.executor(ctx)
	res, err := ex.Exec(`UPDATE robot_assignments SET status = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`, string(status), nowStamp(), id, version)
	if err != nil {
		return 0, fmt.Errorf("update robot assignment status: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) UpdateAssignmentClaim(ctx context.Context, id, claimedBy, leaseID, expires string, version int) (int, error) {
	ex := s.executor(ctx)
	res, err := ex.Exec(`UPDATE robot_assignments SET claimed_by = ?, lease_id = ?, claim_expires_at = ?,
		status = 'claimed', version = version + 1, updated_at = ? WHERE id = ? AND version = ?`,
		claimedBy, leaseID, expires, nowStamp(), id, version)
	if err != nil {
		return 0, fmt.Errorf("update robot assignment claim: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) ClearAssignmentClaim(ctx context.Context, id string, version int) (int, error) {
	ex := s.executor(ctx)
	res, err := ex.Exec(`UPDATE robot_assignments SET claimed_by = '', lease_id = '', claim_expires_at = NULL,
		status = 'preempted', version = version + 1, updated_at = ? WHERE id = ? AND version = ?`,
		nowStamp(), id, version)
	if err != nil {
		return 0, fmt.Errorf("clear robot assignment claim: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) ListClaimableTasks(ctx context.Context, limit int) ([]*domain.RobotAssignment, error) {
	ex := s.executor(ctx)
	rows, err := ex.Query(`SELECT id, mission_id, route_reservation_id, task_type, location, assigned_to,
		claimed_by, claim_expires_at, lease_id, status, priority, report_data, version, created_at, updated_at
		FROM robot_assignments WHERE status = 'assigned' ORDER BY priority ASC, created_at ASC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list claimable tasks: %w", err)
	}
	defer rows.Close()
	var items []*domain.RobotAssignment
	for rows.Next() {
		t, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

func (s *SQLiteStore) ListExpiredClaims(ctx context.Context, now string) ([]*domain.RobotAssignment, error) {
	ex := s.executor(ctx)
	rows, err := ex.Query(`SELECT id, mission_id, route_reservation_id, task_type, location, assigned_to,
		claimed_by, claim_expires_at, lease_id, status, priority, report_data, version, created_at, updated_at
		FROM robot_assignments WHERE claim_expires_at IS NOT NULL AND claim_expires_at < ?
		AND status IN ('claimed', 'in_progress') ORDER BY claim_expires_at ASC`, now)
	if err != nil {
		return nil, fmt.Errorf("list expired claims: %w", err)
	}
	defer rows.Close()
	var items []*domain.RobotAssignment
	for rows.Next() {
		t, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

func (s *SQLiteStore) ListAssignmentsByStatus(ctx context.Context, status domain.AssignmentStatus) ([]*domain.RobotAssignment, error) {
	ex := s.executor(ctx)
	rows, err := ex.Query(`SELECT id, mission_id, route_reservation_id, task_type, location, assigned_to,
		claimed_by, claim_expires_at, lease_id, status, priority, report_data, version, created_at, updated_at
		FROM robot_assignments WHERE status = ? ORDER BY priority ASC, created_at ASC`, string(status))
	if err != nil {
		return nil, fmt.Errorf("list robot assignments by status: %w", err)
	}
	defer rows.Close()
	var items []*domain.RobotAssignment
	for rows.Next() {
		t, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

func (s *SQLiteStore) CountAssignmentsByStatus(ctx context.Context, status domain.AssignmentStatus) (int, error) {
	ex := s.executor(ctx)
	var n int
	err := ex.QueryRow("SELECT COUNT(*) FROM robot_assignments WHERE status = ?", string(status)).Scan(&n)
	return n, err
}
