package smsr

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestMockClient_Success(t *testing.T) {
	mc := NewMockClient()
	mc.FailRate = 0.0

	req := PushRequest{
		EID:         "89000000000000000001",
		CommandType: CommandTypeSwitch,
		CommandID:   uuid.New().String(),
	}

	resp, err := mc.Push(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.SMSRCommandID == "" {
		t.Fatal("expected non-empty SMSRCommandID")
	}
	if _, parseErr := uuid.Parse(resp.SMSRCommandID); parseErr != nil {
		t.Fatalf("SMSRCommandID is not a valid UUID: %s", resp.SMSRCommandID)
	}
	if resp.AcceptedAt.IsZero() {
		t.Fatal("expected non-zero AcceptedAt")
	}

	calls := mc.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 recorded call, got %d", len(calls))
	}
}

func TestMockClient_FailRate_AlwaysFail(t *testing.T) {
	mc := NewMockClient()
	mc.FailRate = 1.0

	_, err := mc.Push(context.Background(), PushRequest{EID: "test", CommandType: CommandTypeEnable})
	if err != ErrSMSRRejected {
		t.Fatalf("expected ErrSMSRRejected, got: %v", err)
	}
}

func TestMockClient_FailRate_NeverFail(t *testing.T) {
	mc := NewMockClient()
	mc.FailRate = 0.0

	for i := 0; i < 20; i++ {
		_, err := mc.Push(context.Background(), PushRequest{EID: "test", CommandType: CommandTypeDisable})
		if err != nil {
			t.Fatalf("iteration %d: expected no error, got: %v", i, err)
		}
	}
}

func TestMockClient_DeterministicUUID(t *testing.T) {
	mc := NewMockClient()
	mc.FailRate = 0.0

	seen := make(map[string]bool)
	for i := 0; i < 10; i++ {
		resp, err := mc.Push(context.Background(), PushRequest{EID: "test", CommandType: CommandTypeSwitch})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if seen[resp.SMSRCommandID] {
			t.Fatalf("duplicate SMSRCommandID: %s", resp.SMSRCommandID)
		}
		seen[resp.SMSRCommandID] = true
	}
}

func TestMockClient_ConcurrentSafety(t *testing.T) {
	mc := NewMockClient()
	mc.FailRate = 0.0

	const goroutines = 50
	var wg sync.WaitGroup
	var errCount atomic.Int64

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := mc.Push(context.Background(), PushRequest{
				EID:         "89000000000000000001",
				CommandType: CommandTypeSwitch,
			})
			if err != nil {
				errCount.Add(1)
			}
		}()
	}
	wg.Wait()

	calls := mc.Calls()
	if len(calls) != goroutines {
		t.Fatalf("expected %d recorded calls, got %d", goroutines, len(calls))
	}
	if errCount.Load() != 0 {
		t.Fatalf("expected 0 errors, got %d", errCount.Load())
	}
}

func TestMockClient_Health(t *testing.T) {
	mc := NewMockClient()
	if err := mc.Health(context.Background()); err != nil {
		t.Fatalf("expected Health to return nil, got: %v", err)
	}
}

func TestMockClient_Reset(t *testing.T) {
	mc := NewMockClient()
	mc.Push(context.Background(), PushRequest{EID: "test", CommandType: CommandTypeEnable})
	mc.Reset()
	if len(mc.Calls()) != 0 {
		t.Fatal("expected 0 calls after Reset")
	}
}
