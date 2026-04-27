package smsr

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

type CallRecord struct {
	Req  PushRequest
	Resp PushResponse
	Err  error
	At   time.Time
}

type MockClient struct {
	FailRate float64
	Latency  time.Duration

	mu      sync.Mutex
	calls   []CallRecord
	randSrc *rand.Rand
}

func NewMockClient() *MockClient {
	failRate := 0.0
	if s := os.Getenv("MOCK_SMSR_FAIL_RATE"); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			failRate = v
		}
	}
	return &MockClient{
		FailRate: failRate,
		randSrc:  rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (m *MockClient) Push(ctx context.Context, req PushRequest) (PushResponse, error) {
	if m.Latency > 0 {
		select {
		case <-time.After(m.Latency):
		case <-ctx.Done():
			return PushResponse{}, fmt.Errorf("smsr mock: context canceled: %w", ctx.Err())
		}
	}

	m.mu.Lock()
	shouldFail := m.FailRate > 0 && m.randSrc.Float64() < m.FailRate
	m.mu.Unlock()

	var resp PushResponse
	var callErr error

	if shouldFail {
		callErr = ErrSMSRRejected
	} else {
		resp = PushResponse{
			SMSRCommandID: uuid.New().String(),
			AcceptedAt:    time.Now().UTC(),
		}
	}

	m.mu.Lock()
	m.calls = append(m.calls, CallRecord{Req: req, Resp: resp, Err: callErr, At: time.Now().UTC()})
	m.mu.Unlock()

	return resp, callErr
}

func (m *MockClient) Health(ctx context.Context) error {
	return nil
}

func (m *MockClient) Calls() []CallRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]CallRecord, len(m.calls))
	copy(out, m.calls)
	return out
}

func (m *MockClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = nil
}
