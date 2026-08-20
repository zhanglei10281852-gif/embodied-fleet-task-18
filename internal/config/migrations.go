package config

import "path/filepath"

// MigrationsSource returns the path to the migration SQL files.
// Defaults to ./migrations relative to the working directory.
func (c *Config) MigrationsSource() string {
	return filepath.Join(".", "migrations")
}
