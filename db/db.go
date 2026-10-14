package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/zanehu-ai/x-go/config"
)

// New initializes a MySQL connection pool via GORM with the given config.
func New(cfg config.MySQLConfig) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("connect mysql: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}

// OpenMySQL opens a GORM DB connection with sensible pool defaults.
func OpenMySQL(dsn string) (*gorm.DB, error) {
	return New(config.MySQLConfig{DSN: dsn})
}

// Probe pings the underlying *sql.DB. nil handles return an error so a
// missing-DSN startup path is reflected as unready.
func Probe(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return errors.New("db: nil handle")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}
