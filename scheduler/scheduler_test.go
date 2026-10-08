package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"

	"github.com/zanehu-ai/x-go/lock"
)

func TestScheduler_ExecutesTask(t *testing.T) {
	var count int64

	s := New(nil)
	s.Register(Task{
		Name:     "counter",
		Interval: 50 * time.Millisecond,
		Fn: func(ctx context.Context) error {
			atomic.AddInt64(&count, 1)
			return nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	s.Start(ctx)

	c := atomic.LoadInt64(&count)
	if c < 2 {
		t.Errorf("expected at least 2 executions, got %d", c)
	}
}

func TestScheduler_MultipleTasks(t *testing.T) {
	var countA, countB int64

	s := New(nil)
	s.Register(Task{
		Name:     "taskA",
		Interval: 50 * time.Millisecond,
		Fn: func(ctx context.Context) error {
			atomic.AddInt64(&countA, 1)
			return nil
		},
	})
	s.Register(Task{
		Name:     "taskB",
		Interval: 50 * time.Millisecond,
		Fn: func(ctx context.Context) error {
			atomic.AddInt64(&countB, 1)
			return nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	s.Start(ctx)

	a := atomic.LoadInt64(&countA)
	b := atomic.LoadInt64(&countB)
	if a < 2 || b < 2 {
		t.Errorf("expected ≥2 each, got A=%d B=%d", a, b)
	}
}

func TestScheduler_StopsOnCancel(t *testing.T) {
	s := New(nil)
	s.Register(Task{
		Name:     "forever",
		Interval: 10 * time.Millisecond,
		Fn:       func(ctx context.Context) error { return nil },
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		s.Start(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("scheduler did not stop after cancel")
	}
}

func TestScheduler_InvalidTask_NoPanic(t *testing.T) {
	noop := func(ctx context.Context) error { return nil }
	tests := []struct {
		name string
		task Task
	}{
		{"zero interval", Task{Name: "zero", Interval: 0, Fn: noop}},
		{"negative interval", Task{Name: "neg", Interval: -1 * time.Second, Fn: noop}},
		{"nil function", Task{Name: "nilFn", Interval: 50 * time.Millisecond, Fn: nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(nil)
			s.Register(tt.task)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			s.Start(ctx)
		})
	}
}

func TestScheduler_WithLockKey(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	locker := lock.New(rdb)
	var count int64

	s := New(locker)
	s.Register(Task{
		Name:     "locked-task",
		Interval: 50 * time.Millisecond,
		LockKey:  "sched:locked-task",
		Fn: func(ctx context.Context) error {
			atomic.AddInt64(&count, 1)
			return nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	s.Start(ctx)

	c := atomic.LoadInt64(&count)
	if c < 2 {
		t.Errorf("expected at least 2 executions with lock, got %d", c)
	}
}
