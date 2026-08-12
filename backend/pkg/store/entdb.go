package store

import (
	"context"
	dql "database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"

	"github.com/teteekoue/NemesisCode/backend/config"
	"github.com/teteekoue/NemesisCode/backend/db"
	dbmigrate "github.com/teteekoue/NemesisCode/backend/db/migrate"
	_ "github.com/teteekoue/NemesisCode/backend/db/runtime"
	"github.com/teteekoue/NemesisCode/backend/pkg/entx"
)

// NewEntDBV2 crée le client ent. Driver "sqlite" (mode local) ou "postgres"
// (défaut historique).
func NewEntDBV2(cfg *config.Config, logger *slog.Logger) (*db.Client, error) {
	if cfg.Database.Driver == "sqlite" {
		return newEntSQLite(cfg, logger)
	}
	return newEntPostgres(cfg, logger)
}

func newEntPostgres(cfg *config.Config, logger *slog.Logger) (*db.Client, error) {
	w, err := sql.Open(dialect.Postgres, cfg.Database.Master)
	if err != nil {
		return nil, err
	}
	w.DB().SetMaxOpenConns(cfg.Database.MaxOpenConns)
	w.DB().SetMaxIdleConns(cfg.Database.MaxIdleConns)
	w.DB().SetConnMaxLifetime(time.Duration(cfg.Database.ConnMaxLifetime) * time.Minute)
	// 如果 slave 为空，使用 master 连接字符串
	slaveConnStr := cfg.Database.Slave
	if slaveConnStr == "" {
		slaveConnStr = cfg.Database.Master
	}
	r, err := sql.Open(dialect.Postgres, slaveConnStr)
	if err != nil {
		return nil, err
	}

	r.DB().SetMaxOpenConns(cfg.Database.MaxOpenConns)
	r.DB().SetMaxIdleConns(cfg.Database.MaxIdleConns)
	r.DB().SetConnMaxLifetime(time.Duration(cfg.Database.ConnMaxLifetime) * time.Minute)
	c := db.NewClient(db.Driver(NewMultiDriver(r, w, logger)))
	c.Task.Use(entx.TaskConcurrencyHook)
	if cfg.Debug {
		c = c.Debug()
	}

	return c, nil
}

// newEntSQLite ouvre une base SQLite (fichier) et fait l'auto-migration du
// schéma ent. Aucun service externe requis — pensé pour le mode local
// (Termux, machine nue, sans Docker).
func newEntSQLite(cfg *config.Config, logger *slog.Logger) (*db.Client, error) {
	path := cfg.Database.SQLitePath
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		path = filepath.Join(home, ".nemesiscode", "nemesiscode.db")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite dir: %w", err)
	}

	dsn := "file:" + path + "?_fk=1&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	w, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		return nil, err
	}
	// SQLite : une seule connexion d'écriture pour éviter les verrous
	// (WAL permet les lectures concurrentes ; le multi-driver n'est pas utilisé).
	w.DB().SetMaxOpenConns(1)

	c := db.NewClient(db.Driver(w))
	c.Task.Use(entx.TaskConcurrencyHook)
	if cfg.Debug {
		c = c.Debug()
	}

	// Auto-migration du schéma (les fichiers SQL migration/ sont en dialecte
	// Postgres et ne s'appliquent pas à SQLite). dbmigrate = package généré
	// par entc (ré-exporte les options de entgo.io/ent/dialect/sql/schema).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := c.Schema.Create(ctx, dbmigrate.WithGlobalUniqueID(false)); err != nil {
		return nil, fmt.Errorf("sqlite schema create: %w", err)
	}
	logger.Info("sqlite database ready", "path", path)
	return c, nil
}

func MigrateSQL(cfg *config.Config, logger *slog.Logger) error {
	if cfg.Database.Driver == "sqlite" {
		// L'auto-migration ent est faite dans NewEntDBV2 (newEntSQLite).
		return nil
	}
	db, err := dql.Open("postgres", cfg.Database.Master)
	if err != nil {
		return err
	}
	defer db.Close()

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithDatabaseInstance(
		"file://migration",
		"postgres", driver)
	if err != nil {
		return err
	}
	defer m.Close()

	return runMigration(m, logger)
}

type migrator interface {
	Up() error
}

func runMigration(m migrator, logger *slog.Logger) error {
	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			logger.With("component", "db").Debug("database schema is up to date")
			return nil
		}
		logger.With("component", "db").With("err", err).Error("migrate db failed")
		return err
	}

	return nil
}
