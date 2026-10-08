package scheduler

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/zanehu-ai/x-go/lock"
	"github.com/zanehu-ai/x-go/logger"
)

// Task defines a periodic background task.
type Task struct {
	Name     string        // unique task name
	Interval time.Duration // execution interval
	Fn       func(ctx context.Context) error
	LockKey  string // optional distributed lock key; if set, only one instance runs the task
}

// Scheduler manages periodic background tasks.
type Scheduler struct {
	tasks  []Task
	locker *lock.Locker // optional, for distributed lock support
	wg     sync.WaitGroup
}

// New creates a Scheduler. Pass nil for locker if distributed locking is not needed.
func New(locker *lock.Locker) *Scheduler {
	return &Scheduler{locker: locker}
}

// Register adds a task to the scheduler. Must be called before Start.
func (s *Scheduler) Register(task Task) {
	s.tasks = append(s.tasks, task)
}

// Start launches all registered tasks as background goroutines.
// Blocks until ctx is cancelled, then waits for all tasks to finish.
func (s *Scheduler) Start(ctx context.Context) {
	for _, task := range s.tasks {
		s.wg.Add(1)
		go s.run(ctx, task)
	}
	s.wg.Wait()
}

func (s *Scheduler) run(ctx context.Context, task Task) {
	defer s.wg.Done()

	if task.Interval <= 0 {
		logger.Error("scheduler: invalid interval, skipping task",
			zap.String("task", task.Name), zap.Duration("interval", task.Interval))
		return
	}
	if task.Fn == nil {
		logger.Error("scheduler: nil function, skipping task",
			zap.String("task", task.Name))
		return
	}

	ticker := time.NewTicker(task.Interval)
	defer ticker.Stop()

	logger.Info("scheduler: task started", zap.String("task", task.Name), zap.Duration("interval", task.Interval))

	for {
		select {
		case <-ctx.Done():
			logger.Info("scheduler: task stopped", zap.String("task", task.Name))
			return
		case <-ticker.C:
			s.execute(ctx, task)
		}
	}
}

func (s *Scheduler) execute(ctx context.Context, task Task) {
	if task.LockKey != "" && s.locker != nil {
		err := s.locker.ExecuteWithLock(ctx, task.LockKey, task.Interval, func() error {
			return task.Fn(ctx)
		})
		if err != nil {
			if err != lock.ErrLockNotAcquired {
				logger.Error("scheduler: task failed", zap.String("task", task.Name), zap.Error(err))
			}
			// ErrLockNotAcquired means another instance is running this task — silently skip
		}
		return
	}

	if err := task.Fn(ctx); err != nil {
		logger.Error("scheduler: task failed", zap.String("task", task.Name), zap.Error(err))
	}
}
