package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds all service configuration values.
type Config struct {
	Server    ServerConfig
	Database  DatabaseConfig
	Auth      AuthConfig
	Scheduler SchedulerConfig
	Executor  ExecutorConfig
	Logging   LoggingConfig
	Quotas    QuotaConfig
}

type AuthConfig struct {
	SessionTTL         time.Duration
	PasswordCost       int
	DispatcherUsername string
	DispatcherPassword string
	EngineerUsername   string
	EngineerPassword   string
	AuditorUsername    string
	AuditorPassword    string
}

type ServerConfig struct {
	Port            int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

type DatabaseConfig struct {
	DataDir      string
	DBName       string
	MaxOpenConns int
	MaxIdleConns int
}

type SchedulerConfig struct {
	TickInterval       time.Duration
	LeaseTimeout       time.Duration
	EscalationInterval time.Duration
	MaxRetries         int
}

type ExecutorConfig struct {
	PollInterval time.Duration
	BatchSize    int
	ID           string
}

type LoggingConfig struct {
	Level  string
	Pretty bool
}

type QuotaConfig struct {
	DailyFastChargeLimit           int
	DailyStandardChargeLimit       int
	FastChargeWarningThreshold     float64
	StandardChargeWarningThreshold float64
}

// Default returns a Config with production-safe defaults.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Port:            58552,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 15 * time.Second,
		},
		Database: DatabaseConfig{
			DataDir:      "./data",
			DBName:       "robotfleet.db",
			MaxOpenConns: 10,
			MaxIdleConns: 5,
		},
		Auth: AuthConfig{
			SessionTTL:         12 * time.Hour,
			PasswordCost:       12,
			DispatcherUsername: "dispatcher",
			DispatcherPassword: "dispatch123",
			EngineerUsername:   "engineer",
			EngineerPassword:   "engineer123",
			AuditorUsername:    "auditor",
			AuditorPassword:    "auditor123",
		},
		Scheduler: SchedulerConfig{
			TickInterval:       5 * time.Second,
			LeaseTimeout:       60 * time.Second,
			EscalationInterval: 10 * time.Second,
			MaxRetries:         3,
		},
		Executor: ExecutorConfig{
			PollInterval: 3 * time.Second,
			BatchSize:    10,
			ID:           "executor-1",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Pretty: true,
		},
		Quotas: QuotaConfig{
			DailyFastChargeLimit:           1000,
			DailyStandardChargeLimit:       5000,
			FastChargeWarningThreshold:     0.8,
			StandardChargeWarningThreshold: 0.8,
		},
	}
}

// FromEnv builds a Config from environment variables, falling back to defaults.
func FromEnv() *Config {
	c := Default()
	if v := os.Getenv("ROBOTFLEET_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Server.Port = p
		}
	}
	if v := os.Getenv("ROBOTFLEET_DATA_DIR"); v != "" {
		c.Database.DataDir = v
	}
	if v := os.Getenv("ROBOTFLEET_DB_NAME"); v != "" {
		c.Database.DBName = v
	}
	if v := os.Getenv("ROBOTFLEET_SESSION_TTL_MINUTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Auth.SessionTTL = time.Duration(n) * time.Minute
		}
	}
	if v := os.Getenv("ROBOTFLEET_PASSWORD_COST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Auth.PasswordCost = n
		}
	}
	setIfPresent(&c.Auth.DispatcherUsername, "ROBOTFLEET_DISPATCHER_USERNAME")
	setIfPresent(&c.Auth.DispatcherPassword, "ROBOTFLEET_DISPATCHER_PASSWORD")
	setIfPresent(&c.Auth.EngineerUsername, "ROBOTFLEET_ENGINEER_USERNAME")
	setIfPresent(&c.Auth.EngineerPassword, "ROBOTFLEET_ENGINEER_PASSWORD")
	setIfPresent(&c.Auth.AuditorUsername, "ROBOTFLEET_AUDITOR_USERNAME")
	setIfPresent(&c.Auth.AuditorPassword, "ROBOTFLEET_AUDITOR_PASSWORD")
	if v := os.Getenv("ROBOTFLEET_REQUEST_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Server.ReadTimeout = time.Duration(n) * time.Second
			c.Server.WriteTimeout = time.Duration(n) * time.Second
		}
	}
	if v := os.Getenv("ROBOTFLEET_SCHEDULER_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Scheduler.TickInterval = time.Duration(n) * time.Second
		}
	}
	if v := os.Getenv("ROBOTFLEET_LEASE_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Scheduler.LeaseTimeout = time.Duration(n) * time.Second
		}
	}
	if v := os.Getenv("ROBOTFLEET_ESCALATION_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Scheduler.EscalationInterval = time.Duration(n) * time.Second
		}
	}
	if v := os.Getenv("ROBOTFLEET_LOG_LEVEL"); v != "" {
		c.Logging.Level = strings.ToLower(v)
	}
	if v := os.Getenv("ROBOTFLEET_EXECUTOR_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Executor.PollInterval = time.Duration(n) * time.Second
		}
	}
	if v := os.Getenv("ROBOTFLEET_EXECUTOR_ID"); v != "" {
		c.Executor.ID = v
	}
	return c
}

// Validate checks the Config for obvious errors.
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Server.Port)
	}
	if c.Database.DataDir == "" {
		return fmt.Errorf("data_dir must not be empty")
	}
	if c.Database.DBName == "" {
		return fmt.Errorf("db_name must not be empty")
	}
	if c.Auth.SessionTTL < time.Minute {
		return fmt.Errorf("session_ttl must be at least one minute")
	}
	if c.Auth.PasswordCost < 4 || c.Auth.PasswordCost > 14 {
		return fmt.Errorf("password_cost must be between 4 and 14")
	}
	if c.Auth.DispatcherUsername == "" || c.Auth.DispatcherPassword == "" ||
		c.Auth.EngineerUsername == "" || c.Auth.EngineerPassword == "" ||
		c.Auth.AuditorUsername == "" || c.Auth.AuditorPassword == "" {
		return fmt.Errorf("bootstrap auth credentials must not be empty")
	}
	if !strings.HasSuffix(c.Database.DBName, ".db") {
		return fmt.Errorf("db_name must end with .db")
	}
	if c.Scheduler.TickInterval < 100*time.Millisecond {
		return fmt.Errorf("scheduler tick_interval too small")
	}
	if c.Scheduler.LeaseTimeout < 1*time.Second {
		return fmt.Errorf("lease timeout too small")
	}
	if c.Executor.BatchSize < 1 {
		return fmt.Errorf("executor batch_size must be >= 1")
	}
	if c.Quotas.DailyFastChargeLimit < 1 || c.Quotas.DailyStandardChargeLimit < 1 {
		return fmt.Errorf("quota limits must be positive")
	}
	return nil
}

func setIfPresent(target *string, name string) {
	if value := os.Getenv(name); value != "" {
		*target = value
	}
}

// DBPath returns the full path to the SQLite database file.
func (c *Config) DBPath() string {
	return filepath.Join(c.Database.DataDir, c.Database.DBName)
}

// EnsureDataDir creates the data directory if it does not exist.
func (c *Config) EnsureDataDir() error {
	if err := os.MkdirAll(c.Database.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	return nil
}
