package mariadb

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
	idle := cfg.ConnMaxIdleTime
	if idle <= 0 || idle > 30*time.Second {
		idle = 30 * time.Second
	}
	db.SetConnMaxIdleTime(idle)

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
	if err := goose.SetDialect("mysql"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		lastErr = goose.RunContext(context.Background(), "up", db, dir)
		if lastErr == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
	}
	return fmt.Errorf("failed to run migrations: %w", lastErr)
}

func buildDSN(cfg *config.DBConfig) string {
	host := cfg.Host
	if host == "localhost" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&multiStatements=true&timeout=5s&readTimeout=5s&writeTimeout=5s",
		cfg.Username,
		cfg.Password,
		host,
		cfg.Port,
		cfg.Name,
	)
}
