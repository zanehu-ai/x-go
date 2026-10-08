package outbox_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/zanehu-ai/x-go/outbox"
)

// Compile-time checks: both concrete types must satisfy the Inserter interface.
var (
	_ outbox.Inserter = (*outbox.RepoInserter)(nil)
	_ outbox.Inserter = (*outbox.MockInserter)(nil)
)

// ---------------------------------------------------------------------------
// RepoInserter construction
// ---------------------------------------------------------------------------

func TestNewRepoInserterReturnsConcrete(t *testing.T) {
	ins := outbox.NewRepoInserter()
	if ins == nil {
		t.Fatal("NewRepoInserter() returned nil")
	}
}

// ---------------------------------------------------------------------------
// MockInserter — happy-path recording
// ---------------------------------------------------------------------------

func TestMockInserterRecordsEvent(t *testing.T) {
	m := outbox.NewMockInserter()
	evt := &testEvent{EventType: "order.created", TenantID: 1}
	if err := m.InsertTx(context.Background(), nil, evt); err != nil {
		t.Fatalf("InsertTx returned unexpected error: %v", err)
	}
	events := m.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	got, ok := events[0].(*testEvent)
	if !ok || got.EventType != "order.created" {
		t.Fatalf("recorded event = %+v, want testEvent{order.created}", events[0])
	}
}

func TestMockInserterRecordsMultipleEvents(t *testing.T) {
	m := outbox.NewMockInserter()
	for i := range 5 {
		evt := &testEvent{EventType: "order.paid", TenantID: uint64(i + 1)}
		if err := m.InsertTx(context.Background(), nil, evt); err != nil {
			t.Fatalf("InsertTx[%d] returned error: %v", i, err)
		}
	}
	if n := len(m.Events()); n != 5 {
		t.Fatalf("Events() len = %d, want 5", n)
	}
}

// ---------------------------------------------------------------------------
// MockInserter — failure injection
// ---------------------------------------------------------------------------

func TestMockInserterFailureInjectionBubblesError(t *testing.T) {
	m := outbox.NewMockInserter()
	want := errors.New("simulated outbox write failure")
	m.Err = want

	evt := &testEvent{EventType: "order.cancelled", TenantID: 1}
	err := m.InsertTx(context.Background(), nil, evt)
	if !errors.Is(err, want) {
		t.Fatalf("InsertTx error = %v, want %v", err, want)
	}
	// Nothing should be recorded when the error path fires.
	if n := len(m.Events()); n != 0 {
		t.Fatalf("Events() len = %d, want 0 after failure", n)
	}
}

// ---------------------------------------------------------------------------
// MockInserter — simulates tx rollback semantics
//
// The mock itself doesn't own a real DB tx. This test simulates the caller
// pattern: if the caller's tx is rolled back, the caller simply discards the
// MockInserter; events captured in the mock are never consumed by the
// dispatcher because the mock doesn't persist to DB. The test verifies that
// the caller can inspect captured events and decide to discard them.
// ---------------------------------------------------------------------------

func TestMockInserterSimulatesTxRollbackNoRowPersisted(t *testing.T) {
	m := outbox.NewMockInserter()
	evt := &testEvent{EventType: "payment.failed", TenantID: 42}

	// Simulate: caller calls InsertTx inside a transaction...
	if err := m.InsertTx(context.Background(), nil, evt); err != nil {
		t.Fatalf("InsertTx returned error: %v", err)
	}
	// ...but then decides to roll back (business logic error):
	simulatedRollback := true
	if simulatedRollback {
		// On rollback the outbox row is also gone — represented here by
		// discarding the mock (Reset) so downstream assertions find zero rows.
		m.Reset()
	}
	if n := len(m.Events()); n != 0 {
		t.Fatalf("after simulated rollback Events() len = %d, want 0", n)
	}
}

// Simulates the commit path: InsertTx is called inside a logical tx, the tx
// commits, and the event is visible to the dispatcher.
func TestMockInserterSimulatesTxCommitRowVisible(t *testing.T) {
	m := outbox.NewMockInserter()
	evt := &testEvent{EventType: "payment.succeeded", TenantID: 7}

	// Inside tx:
	if err := m.InsertTx(context.Background(), nil, evt); err != nil {
		t.Fatalf("InsertTx returned error: %v", err)
	}
	// Tx commits → no Reset, event visible to dispatcher:
	if n := len(m.Events()); n != 1 {
		t.Fatalf("after commit Events() len = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// MockInserter — concurrent safety (race detector must not fire)
// ---------------------------------------------------------------------------

func TestMockInserterConcurrentInsertsTwoGoroutines(t *testing.T) {
	m := outbox.NewMockInserter()
	const goroutines = 2
	const eventsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(g int) {
			defer wg.Done()
			for i := range eventsPerGoroutine {
				evt := &testEvent{
					EventType: "concurrent.event",
					TenantID:  uint64(g*100 + i),
				}
				if err := m.InsertTx(context.Background(), nil, evt); err != nil {
					t.Errorf("goroutine %d InsertTx[%d] error: %v", g, i, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	total := len(m.Events())
	if total != goroutines*eventsPerGoroutine {
		t.Fatalf("Events() len = %d, want %d", total, goroutines*eventsPerGoroutine)
	}
}

// ---------------------------------------------------------------------------
// MockInserter — Reset clears state
// ---------------------------------------------------------------------------

func TestMockInserterResetClearsEventsAndError(t *testing.T) {
	m := outbox.NewMockInserter()
	m.Err = errors.New("old error")
	_ = m.InsertTx(context.Background(), nil, &testEvent{EventType: "x", TenantID: 1})

	m.Reset()

	if m.Err != nil {
		t.Fatalf("Err after Reset = %v, want nil", m.Err)
	}
	if n := len(m.Events()); n != 0 {
		t.Fatalf("Events() after Reset = %d, want 0", n)
	}
	// Should record again after Reset.
	if err := m.InsertTx(context.Background(), nil, &testEvent{EventType: "y", TenantID: 2}); err != nil {
		t.Fatalf("InsertTx after Reset returned error: %v", err)
	}
	if n := len(m.Events()); n != 1 {
		t.Fatalf("Events() after post-Reset insert = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// RepoInserter — nil tx surface
//
// InsertTx with a nil tx panics (GORM contract). Document this expectation
// via a recover so the test passes and the behaviour is explicit.
// In production, callers always provide a non-nil tx from db.Transaction().
// ---------------------------------------------------------------------------

func TestRepoInserterNilTxPanics(t *testing.T) {
	ins := outbox.NewRepoInserter()
	evt := &testEvent{EventType: "irrelevant", TenantID: 1}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic with nil tx, but no panic occurred")
		}
	}()
	// This MUST panic — callers are responsible for passing a valid tx.
	_ = ins.InsertTx(context.Background(), nil, evt) //nolint:errcheck
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type testEvent struct {
	EventType string
	TenantID  uint64
}
