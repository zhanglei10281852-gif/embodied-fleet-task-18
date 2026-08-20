package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// DB wraps a *sql.DB with helpers for migrations and transactions.
type DB struct {
	db *sql.DB
}

// Open creates or opens a SQLite database at dbPath and runs migrations.
func Open(dbPath, migrationsDir string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}
	dsn := buildDSN(dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	configurePool(db, dbPath)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	d := &DB{db: db}
	if err := d.Migrate(migrationsDir); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

// OpenDB opens the database without running migrations (for test use).
func OpenDB(dbPath string) (*DB, error) {
	dsn := buildDSN(dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	configurePool(db, dbPath)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return &DB{db: db}, nil
}

func configurePool(db *sql.DB, dbPath string) {
	if dbPath == ":memory:" {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	} else {
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
	}
}

func buildDSN(dbPath string) string {
	return dbPath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
}

// SQL returns the underlying *sql.DB for direct queries.
func (d *DB) SQL() *sql.DB { return d.db }

// Close closes the database connection.
func (d *DB) Close() error { return d.db.Close() }

// InTx executes fn inside a serializable transaction.
func (d *DB) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	txCtx := context.WithValue(ctx, txKey{}, tx)
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := fn(txCtx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	committed = true
	return nil
}

type txKey struct{}

// TxFromContext extracts the *sql.Tx stored by InTx, or returns nil.
func TxFromContext(ctx context.Context) *sql.Tx {
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return tx
	}
	return nil
}

// Migrate reads SQL files from migrationsDir and applies them in order.
func (d *DB) Migrate(migrationsDir string) error {
	if _, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	files, err := migrationFiles(migrationsDir)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := d.applyMigration(f.path, f.version, f.description); err != nil {
			return fmt.Errorf("apply migration %s: %w", f.path, err)
		}
	}
	return nil
}

type migrationFile struct {
	path        string
	version     int
	description string
}

func migrationFiles(dir string) ([]migrationFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	var files []migrationFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		var version int
		var description string
		name := strings.TrimSuffix(e.Name(), ".sql")
		parts := strings.SplitN(name, "_", 2)
		if len(parts) >= 1 {
			if _, err := fmt.Sscanf(parts[0], "%d", &version); err != nil {
				continue
			}
		}
		if len(parts) == 2 {
			description = strings.ReplaceAll(parts[1], "_", " ")
		}
		files = append(files, migrationFile{
			path:        filepath.Join(dir, e.Name()),
			version:     version,
			description: description,
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })
	return files, nil
}

func (d *DB) applyMigration(path string, version int, description string) error {
	var applied int
	row := d.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version)
	if err := row.Scan(&applied); err != nil {
		return fmt.Errorf("check migration: %w", err)
	}
	if applied > 0 {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read migration file: %w", err)
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(string(content)); err != nil {
		tx.Rollback()
		return fmt.Errorf("exec migration: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations (version, description) VALUES (?, ?)", version, description); err != nil {
		tx.Rollback()
		return fmt.Errorf("record migration: %w", err)
	}
	return tx.Commit()
}

// AppliedMigrations returns the list of applied migration versions.
func (d *DB) AppliedMigrations() ([]int, error) {
	rows, err := d.db.Query("SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var versions []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}
