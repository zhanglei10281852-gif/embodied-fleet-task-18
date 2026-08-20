package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

func (s *SQLiteStore) CreateUserIfAbsent(ctx context.Context, user *domain.User) error {
	_, err := s.executor(ctx).ExecContext(ctx, `INSERT OR IGNORE INTO users
        (id, username, password_hash, role, active, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)`, user.ID, user.Username, user.PasswordHash,
		user.Role, user.Active, user.CreatedAt.Format(time.RFC3339Nano), user.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("create bootstrap user: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetUserByUsername(ctx context.Context, username string) (*domain.User, error) {
	return scanUser(s.executor(ctx).QueryRowContext(ctx, `SELECT id, username, password_hash, role,
        active, created_at, updated_at FROM users WHERE username = ?`, username))
}

func (s *SQLiteStore) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	return scanUser(s.executor(ctx).QueryRowContext(ctx, `SELECT id, username, password_hash, role,
        active, created_at, updated_at FROM users WHERE id = ?`, id))
}

func scanUser(row *sql.Row) (*domain.User, error) {
	var user domain.User
	var role, createdAt, updatedAt string
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &role, &user.Active, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	user.Role = domain.AuthRole(role)
	user.CreatedAt = parseTime(createdAt)
	user.UpdatedAt = parseTime(updatedAt)
	return &user, nil
}

func (s *SQLiteStore) CreateSession(ctx context.Context, session *domain.Session) error {
	_, err := s.executor(ctx).ExecContext(ctx, `INSERT INTO sessions
        (id, user_id, token_hash, expires_at, revoked_at, created_at, updated_at)
        VALUES (?, ?, ?, ?, NULL, ?, ?)`, session.ID, session.UserID, session.TokenHash,
		session.ExpiresAt.Format(time.RFC3339Nano), session.CreatedAt.Format(time.RFC3339Nano), session.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*domain.Session, error) {
	row := s.executor(ctx).QueryRowContext(ctx, `SELECT id, user_id, token_hash, expires_at,
        revoked_at, created_at, updated_at FROM sessions WHERE token_hash = ?`, tokenHash)
	var session domain.Session
	var expiresAt, createdAt, updatedAt string
	var revokedAt sql.NullString
	if err := row.Scan(&session.ID, &session.UserID, &session.TokenHash, &expiresAt,
		&revokedAt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	session.ExpiresAt = parseTime(expiresAt)
	session.RevokedAt = parseNullTime(revokedAt)
	session.CreatedAt = parseTime(createdAt)
	session.UpdatedAt = parseTime(updatedAt)
	return &session, nil
}

func (s *SQLiteStore) RevokeSession(ctx context.Context, tokenHash string, revokedAt string) (int, error) {
	result, err := s.executor(ctx).ExecContext(ctx, `UPDATE sessions SET revoked_at = ?, updated_at = ?
        WHERE token_hash = ? AND revoked_at IS NULL`, revokedAt, revokedAt, tokenHash)
	if err != nil {
		return 0, fmt.Errorf("revoke session: %w", err)
	}
	count, err := result.RowsAffected()
	return int(count), err
}

func (s *SQLiteStore) DeleteExpiredSessions(ctx context.Context, before string) (int, error) {
	result, err := s.executor(ctx).ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", before)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	count, err := result.RowsAffected()
	return int(count), err
}
