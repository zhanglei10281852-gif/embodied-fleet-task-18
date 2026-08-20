package main

import (
	"fmt"
	"os"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/config"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

func main() {
	cfg := config.FromEnv()
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.EnsureDataDir(); err != nil {
		fmt.Fprintf(os.Stderr, "data dir error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Opening database at %s\n", cfg.DBPath())
	fmt.Printf("Migrations source: %s\n", cfg.MigrationsSource())

	db, err := store.Open(cfg.DBPath(), cfg.MigrationsSource())
	if err != nil {
		fmt.Fprintf(os.Stderr, "migration error: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	versions, err := db.AppliedMigrations()
	if err != nil {
		fmt.Fprintf(os.Stderr, "query migrations error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Migrations applied successfully. Applied versions: %v\n", versions)
}
