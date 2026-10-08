package redis

import (
	"testing"

	"github.com/zanehu-ai/x-go/config"
)

// TC-HAPPY-REDIS-001: connect to Redis with valid config
func TestNew_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("requires external service")
	}
	client, err := New(config.RedisConfig{Addr: "localhost:6379"})
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	defer func() { _ = client.Close() }()
}

// TC-HAPPY-REDIS-002: set and get a key
func TestNew_SetGet(t *testing.T) {
	if testing.Short() {
		t.Skip("requires external service")
	}
	client, err := New(config.RedisConfig{Addr: "localhost:6379"})
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := client.Context()
	key := "test_818_shared:hello"
	defer client.Del(ctx, key)

	if err := client.Set(ctx, key, "world", 0).Err(); err != nil {
		t.Fatalf("SET failed: %v", err)
	}
	val, err := client.Get(ctx, key).Result()
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	if val != "world" {
		t.Errorf("GET = %q, want %q", val, "world")
	}
}

// TC-EXCEPTION-REDIS-001: invalid addr returns error
func TestNew_InvalidAddr(t *testing.T) {
	if testing.Short() {
		t.Skip("requires external service")
	}
	_, err := New(config.RedisConfig{Addr: "localhost:59999"})
	if err == nil {
		t.Error("expected error for invalid addr")
	}
}

// TC-EXCEPTION-REDIS-002: wrong password returns error
func TestNew_WrongPassword(t *testing.T) {
	if testing.Short() {
		t.Skip("requires external service")
	}
	_, err := New(config.RedisConfig{Addr: "localhost:6379", Password: "wrong-password-818"})
	if err == nil {
		t.Error("expected error for wrong password")
	}
}
