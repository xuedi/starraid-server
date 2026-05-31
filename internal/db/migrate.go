// Package db owns the server's PostgreSQL access: schema migrations (this file)
// and, in later slices, the query layer + connection pool. The server owns the
// schema and applies it (see docs/database.md).
package db

import (
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx" for goose
	"github.com/pressly/goose/v3"
)

// migrationFS holds the embedded goose migrations. They live beside this file so
// go:embed can reach them; goose reads them via SetBaseFS.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate applies all pending migrations to the database at dsn, then returns.
// It is idempotent: already-applied migrations are skipped. Safe to call on every
// server startup as well as standalone (cmd/migrate).
func Migrate(dsn string) error {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("db: open %q: %w", dsn, err)
	}
	defer conn.Close()

	goose.SetBaseFS(migrationFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("db: set dialect: %w", err)
	}
	if err := goose.Up(conn, "migrations"); err != nil {
		return fmt.Errorf("db: migrate up: %w", err)
	}
	return nil
}
