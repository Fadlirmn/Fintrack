package db

import (
	_ "embed"
	"log"

	"github.com/jmoiron/sqlx"
)

//go:embed migrations/001_init.sql
var initSQL string

// RunMigrations applies the embedded SQL schema to the database.
// Uses IF NOT EXISTS so it is safe to run on every startup.
func RunMigrations(db *sqlx.DB) {
	log.Println("Running database migrations...")
	if _, err := db.Exec(initSQL); err != nil {
		log.Fatalf("Failed to apply database migrations: %v", err)
	}
	log.Println("Database migrations applied successfully")
}
