package store

import (
	"context"
	"database/sql"
	"fmt"
)

// SQLiteStore implements Store over a *sql.DB backed by modernc.org/sqlite.
type SQLiteStore struct {
	db *DB
}

// NewSQLiteStore wraps an already-opened DB.
func NewSQLiteStore(db *DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}

// OpenStore opens a SQLite database, runs migrations, and returns a Store.
func OpenStore(dbPath, migrationsDir string) (*SQLiteStore, error) {
	db, err := Open(dbPath, migrationsDir)
	if err != nil {
		return nil, err
	}
	return NewSQLiteStore(db), nil
}

// Close closes the underlying database.
func (s *SQLiteStore) Close() error { return s.db.Close() }

// DB exposes the underlying DB for migration tooling.
func (s *SQLiteStore) DB() *DB { return s.db }

// InTx delegates to the DB transaction runner.
func (s *SQLiteStore) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return s.db.InTx(ctx, fn)
}

// executor returns a query executor: the active *sql.Tx if inside one,
// otherwise the bare *sql.DB. This lets repositories run both inside
// and outside transactions without code duplication.
func (s *SQLiteStore) executor(ctx context.Context) DBExecutor {
	if tx := TxFromContext(ctx); tx != nil {
		return tx
	}
	return s.db.SQL()
}

// DBExecutor is satisfied by both *sql.DB and *sql.Tx.
type DBExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// scanInt scans a single integer column.
func scanInt(row *sql.Row) (int, error) {
	var n int
	if err := row.Scan(&n); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}

// buildListQuery constructs a paginated SELECT with optional filters.
func buildListQuery(baseQuery string, q PageParams) (string, []any) {
	var args []any
	query := baseQuery
	if len(q.Filters) > 0 {
		query += " WHERE "
		clauses := make([]string, 0, len(q.Filters))
		for col, val := range q.Filters {
			clauses = append(clauses, fmt.Sprintf("%s = ?", col))
			args = append(args, val)
		}
		query += joinAnd(clauses)
	}
	if q.OrderBy != "" {
		query += fmt.Sprintf(" ORDER BY %s %s", q.OrderBy, q.OrderDir)
	} else {
		query += " ORDER BY created_at DESC"
	}
	args = append(args, q.PageSize, q.Offset)
	query += " LIMIT ? OFFSET ?"
	return query, args
}

// PageParams holds query-building parameters for list queries.
type PageParams struct {
	PageSize int
	Offset   int
	OrderBy  string
	OrderDir string
	Filters  map[string]string
}

func joinAnd(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += " AND "
		}
		result += p
	}
	return result
}

// OptimisticLockConflict returns true when the affected-rows count indicates
// a version mismatch (zero rows updated despite the entity existing).
func OptimisticLockConflict(affected int) bool {
	return affected == 0
}
