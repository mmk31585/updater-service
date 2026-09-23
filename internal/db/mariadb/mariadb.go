package mariadb

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
	"github.com/mmk31585/updater-service/internal/config"
	"github.com/pressly/goose/v3"
)

func New(cfg *config.DBConfig) (*sql.DB, error) {
	dsn := buildDSN(cfg)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return db, nil
}

func Migrate(db *sql.DB, dir string) error {
	if dir == "" {
		dir = "./migrations"
	}
	return goose.Run("up", db, dir)
}

func buildDSN(cfg *config.DBConfig) string {
	host := cfg.Host
	if host == "localhost" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&multiStatements=true",
		cfg.Username,
		cfg.Password,
		host,
		cfg.Port,
		cfg.Name,
	)
}
