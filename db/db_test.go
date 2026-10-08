package db

import (
	"context"
	"testing"

	"github.com/zanehu-ai/x-go/config"
)

const testDSN = "root:root@tcp(127.0.0.1:3306)/test_818_shared?parseTime=true&charset=utf8mb4"

// TC-HAPPY-DB-001: connect to MySQL with valid DSN
func TestNew_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("requires external service")
	}
	db, err := New(config.MySQLConfig{DSN: testDSN})
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping failed: %v", err)
	}
	_ = sqlDB.Close()
}

func TestProbeNilHandle(t *testing.T) {
	if err := Probe(context.Background(), nil); err == nil {
		t.Fatalf("Probe(nil) err = %v, want non-nil", err)
	}
}

// TC-HAPPY-DB-002: connection pool settings applied
func TestNew_PoolSettings(t *testing.T) {
	if testing.Short() {
		t.Skip("requires external service")
	}
	db, err := New(config.MySQLConfig{DSN: testDSN})
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	sqlDB, _ := db.DB()
	defer func() { _ = sqlDB.Close() }()

	if sqlDB.Stats().MaxOpenConnections != 100 {
		t.Errorf("MaxOpenConnections = %d, want 100", sqlDB.Stats().MaxOpenConnections)
	}
}

// TC-EXCEPTION-DB-001: invalid DSN returns error
func TestNew_InvalidDSN(t *testing.T) {
	if testing.Short() {
		t.Skip("requires external service")
	}
	_, err := New(config.MySQLConfig{DSN: "invalid:invalid@tcp(localhost:9999)/nope"})
	if err == nil {
		t.Error("expected error for invalid DSN")
	}
}

// TC-EXCEPTION-DB-002: empty DSN returns error
func TestNew_EmptyDSN(t *testing.T) {
	if testing.Short() {
		t.Skip("requires external service")
	}
	_, err := New(config.MySQLConfig{DSN: ""})
	if err == nil {
		t.Error("expected error for empty DSN")
	}
}
