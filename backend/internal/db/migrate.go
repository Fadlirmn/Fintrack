package db

import (
	_ "embed"
	"log"

	"github.com/jmoiron/sqlx"
)

//go:embed migrations/001_init.sql
var initSQL string

//go:embed migrations/002_add_fixed_expenses.sql
var fixedExpensesSQL string

// RunMigrations applies all embedded SQL migrations to the database.
// Uses IF NOT EXISTS / ADD COLUMN IF NOT EXISTS so safe to run on every startup.
func RunMigrations(db *sqlx.DB) {
	log.Println("Running database migrations...")

	if _, err := db.Exec(initSQL); err != nil {
		log.Fatalf("Failed to apply migration 001_init: %v", err)
	}

	if _, err := db.Exec(fixedExpensesSQL); err != nil {
		log.Fatalf("Failed to apply migration 002_add_fixed_expenses: %v", err)
	}

	log.Println("Database migrations applied successfully")
}
