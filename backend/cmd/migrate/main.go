// Command migrate applies the SQL migrations in backend/migrations to the
// database named by YMCA_DATABASE_URL.
//
//	migrate up        apply every pending migration
//	migrate status    list applied and pending migrations
//	migrate version   print the current schema version
//
// There is no `down`. Migrations are forward-only (07_deployment_view.md
// §7.4) and no migration file declares a Down section, so goose would fail
// on the attempt anyway. This command refuses earlier and says why.
//
// The role in YMCA_DATABASE_URL owns the resulting tables and therefore
// needs DDL. That is not the role the API should use — see the note on
// Config.DatabaseURL and backend/README.md.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"

	"github.com/nandgator/ymca/backend/internal/config"
	"github.com/nandgator/ymca/backend/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: migrate <up|status|version>")
	}
	command := os.Args[1]

	if command == "down" || command == "reset" || command == "redo" {
		return fmt.Errorf(
			"%q is not available: migrations are forward-only (07.4). "+
				"To reset a development database, drop and recreate it, then run `migrate up`",
			command)
	}

	cfg, err := config.LoadDatabaseOnly()
	if err != nil {
		return err
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}

	// "." because embed.FS in the migrations package is rooted at that
	// directory's contents.
	const dir = "."

	switch command {
	case "up":
		return goose.UpContext(ctx, db, dir)
	case "status":
		return goose.StatusContext(ctx, db, dir)
	case "version":
		return goose.VersionContext(ctx, db, dir)
	default:
		return fmt.Errorf("unknown command %q: expected up, status or version", command)
	}
}
